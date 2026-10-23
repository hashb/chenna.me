package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"go.hacdias.com/indielib/micropub"
)

const maxMicropubBodySize int64 = 2 << 20 // 2 MiB for non-media Micropub requests.

type mediaUploadFunc func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error)

// requestAuthorizer reports whether a request may perform a multipart create.
// It returns errUnauthorized when no token was supplied and
// errInsufficientScope when the token could not be verified for "create".
type requestAuthorizer func(r *http.Request) error

var (
	errUnauthorized      = errors.New("missing access token")
	errInsufficientScope = errors.New("insufficient scope")
)

// preauthorizedScopeKey marks a rewritten request whose token was already
// verified (for the scope stored as the value) before media was uploaded, so
// indielib's own scope check does not hit the token endpoint a second time.
// Context values cannot be set by clients, so this cannot be spoofed.
type preauthorizedScopeKey struct{}

// micropubRouter wraps jekyllMicropub so HasScope honours a scope that the
// multipart pre-check already verified for this request.
type micropubRouter struct {
	*jekyllMicropub
}

func (m micropubRouter) HasScope(r *http.Request, scope string) bool {
	if verified, ok := r.Context().Value(preauthorizedScopeKey{}).(string); ok && verified == scope {
		return true
	}
	return m.jekyllMicropub.HasScope(r, scope)
}

func newMicropubHandler(impl *jekyllMicropub) http.Handler {
	return newMicropubHandlerWithUpload(impl, multipartCreateAuthorizer(impl.HasScope), impl.uploadMedia)
}

func newMicropubHandlerWithUpload(impl *jekyllMicropub, authorize requestAuthorizer, upload mediaUploadFunc) http.Handler {
	handler := micropub.NewHandler(micropubRouter{impl},
		micropub.WithMediaEndpoint(impl.endpointURL+"/media"),
		micropub.WithGetCategories(impl.getCategories),
		micropub.WithGetPostStatuses(func() []string {
			return []string{"published", "draft"}
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// indielib does not authorize source queries. Reading drafts and post
		// source is part of editing, so require the owner's update scope.
		if r.Method == http.MethodGet && r.URL.Query().Get("q") == "source" {
			if extractBearerToken(r) == "" {
				serveMicropubError(w, errUnauthorized)
				return
			}
			if !impl.HasScope(r, "update") {
				serveMicropubError(w, errInsufficientScope)
				return
			}
		}
		if isMultipartMicropubRequest(r) {
			rewritten, err := rewriteMultipartCreateRequest(w, r, authorize, upload)
			if err != nil {
				serveMicropubError(w, err)
				return
			}
			handler.ServeHTTP(w, rewritten)
			return
		}

		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxMicropubBodySize)
		}

		handler.ServeHTTP(w, r)
	})
}

// multipartCreateAuthorizer checks the "create" scope for a multipart request.
// It must run after the multipart form is parsed, because the token may be
// sent as the access_token form field instead of the Authorization header.
func multipartCreateAuthorizer(hasScope func(r *http.Request, scope string) bool) requestAuthorizer {
	return func(r *http.Request) error {
		if extractBearerToken(r) == "" {
			return errUnauthorized
		}
		if !hasScope(r, "create") {
			return errInsufficientScope
		}
		return nil
	}
}

func isMultipartMicropubRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data")
}

// rewriteMultipartCreateRequest uploads any photo files in a multipart create
// request and rewrites it as a form-encoded request referencing the uploaded
// URLs. The request is authorized before any file is processed or uploaded:
// indielib only checks scopes once it handles the rewritten request, which is
// too late to stop unauthenticated uploads to the public CDN.
func rewriteMultipartCreateRequest(w http.ResponseWriter, r *http.Request, authorize requestAuthorizer, upload mediaUploadFunc) (*http.Request, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaSize)
	if err := r.ParseMultipartForm(0); err != nil {
		return nil, fmt.Errorf("%w: %w", micropub.ErrBadRequest, err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	values := cloneFormValues(r.MultipartForm.Value)
	if len(r.MultipartForm.File) == 0 {
		// No side effects here; indielib checks the scope as usual.
		return rewriteAsFormURLEncodedRequest(r, values), nil
	}

	if err := authorize(r); err != nil {
		return nil, err
	}

	if values.Get("action") != "" {
		return nil, fmt.Errorf("%w: multipart Micropub file uploads only support create requests", micropub.ErrBadRequest)
	}
	if values.Get("h") == "" {
		return nil, fmt.Errorf("%w: multipart Micropub file uploads require an h=* create request", micropub.ErrBadRequest)
	}

	for key, headers := range r.MultipartForm.File {
		if strings.TrimSuffix(key, "[]") != "photo" {
			return nil, fmt.Errorf("%w: file uploads are only supported for the photo property", micropub.ErrBadRequest)
		}

		for _, header := range headers {
			file, err := header.Open()
			if err != nil {
				return nil, fmt.Errorf("opening upload %q: %w", header.Filename, err)
			}

			location, uploadErr := upload(r.Context(), file, header)
			closeErr := file.Close()
			if uploadErr != nil {
				return nil, errors.Join(fmt.Errorf("uploading photo %q: %w", header.Filename, uploadErr), closeErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("closing upload %q: %w", header.Filename, closeErr)
			}

			values.Add(key, location)
		}
	}

	rewritten := rewriteAsFormURLEncodedRequest(r, values)
	return rewritten.WithContext(context.WithValue(rewritten.Context(), preauthorizedScopeKey{}, "create")), nil
}

func cloneFormValues(src map[string][]string) url.Values {
	values := make(url.Values, len(src))
	for key, items := range src {
		values[key] = append([]string(nil), items...)
	}
	return values
}

func rewriteAsFormURLEncodedRequest(r *http.Request, values url.Values) *http.Request {
	encoded := values.Encode()
	rewritten := r.Clone(r.Context())
	rewritten.Header = r.Header.Clone()
	rewritten.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rewritten.Header.Del("Content-Length")
	rewritten.Body = io.NopCloser(strings.NewReader(encoded))
	rewritten.ContentLength = int64(len(encoded))
	rewritten.Form = nil
	rewritten.PostForm = nil
	rewritten.MultipartForm = nil
	rewritten.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(encoded)), nil
	}
	return rewritten
}

func serveMicropubError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	errorCode := "server_error"

	switch {
	case errors.Is(err, errUnauthorized):
		status = http.StatusUnauthorized
		errorCode = "unauthorized"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(err, errInsufficientScope):
		// Same shape indielib uses for failed scope checks.
		status = http.StatusForbidden
		errorCode = "insufficient_scope"
	case errors.Is(err, micropub.ErrBadRequest):
		status = http.StatusBadRequest
		errorCode = "invalid_request"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             errorCode,
		"error_description": err.Error(),
	})
}
