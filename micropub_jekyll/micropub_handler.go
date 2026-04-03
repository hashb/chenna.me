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

func newMicropubHandler(impl *jekyllMicropub) http.Handler {
	handler := micropub.NewHandler(impl,
		micropub.WithMediaEndpoint(impl.siteURL+"/media"),
		micropub.WithGetCategories(impl.getCategories),
		micropub.WithGetPostStatuses(func() []string {
			return []string{"published", "draft"}
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMultipartMicropubRequest(r) {
			rewritten, err := rewriteMultipartCreateRequest(w, r, impl.uploadMedia)
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

func isMultipartMicropubRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data")
}

func rewriteMultipartCreateRequest(w http.ResponseWriter, r *http.Request, upload mediaUploadFunc) (*http.Request, error) {
	r.Body = http.MaxBytesReader(w, r.Body, micropub.DefaultMaxMediaSize)
	if err := r.ParseMultipartForm(0); err != nil {
		return nil, fmt.Errorf("%w: %w", micropub.ErrBadRequest, err)
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	values := cloneFormValues(r.MultipartForm.Value)
	if len(r.MultipartForm.File) == 0 {
		return rewriteAsFormURLEncodedRequest(r, values), nil
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

	return rewriteAsFormURLEncodedRequest(r, values), nil
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

	if errors.Is(err, micropub.ErrBadRequest) {
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
