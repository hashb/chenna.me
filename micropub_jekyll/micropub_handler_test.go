package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.hacdias.com/indielib/micropub"
)

func allowAll(*http.Request) error { return nil }

func TestRewriteMultipartCreateRequestUploadsPhotos(t *testing.T) {
	t.Parallel()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("h", "entry"); err != nil {
		t.Fatalf("WriteField h: %v", err)
	}
	if err := writer.WriteField("content", "hello, world"); err != nil {
		t.Fatalf("WriteField content: %v", err)
	}
	if err := writer.WriteField("category[]", "photos"); err != nil {
		t.Fatalf("WriteField category: %v", err)
	}

	part, err := writer.CreateFormFile("photo[]", "one.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile one.jpg: %v", err)
	}
	if _, err := part.Write([]byte("one")); err != nil {
		t.Fatalf("write one.jpg: %v", err)
	}

	part, err = writer.CreateFormFile("photo[]", "two.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile two.jpg: %v", err)
	}
	if _, err := part.Write([]byte("two")); err != nil {
		t.Fatalf("write two.jpg: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var uploaded []string
	rewritten, err := rewriteMultipartCreateRequest(httptest.NewRecorder(), req, allowAll, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
		data, err := io.ReadAll(file)
		if err != nil {
			return "", err
		}
		uploaded = append(uploaded, header.Filename+":"+string(data))
		return "https://i.example/" + header.Filename, nil
	})
	if err != nil {
		t.Fatalf("rewriteMultipartCreateRequest: %v", err)
	}

	if got := rewritten.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("rewritten Content-Type = %q", got)
	}

	parsed, err := micropub.ParseRequest(rewritten)
	if err != nil {
		t.Fatalf("ParseRequest(rewritten): %v", err)
	}

	if parsed.Action != micropub.ActionCreate {
		t.Fatalf("parsed.Action = %q, want %q", parsed.Action, micropub.ActionCreate)
	}
	if got := extractContent(parsed.Properties); got != "hello, world" {
		t.Fatalf("extractContent = %q, want %q", got, "hello, world")
	}
	if got := extractStringSlice(parsed.Properties, "category"); len(got) != 1 || got[0] != "photos" {
		t.Fatalf("categories = %#v, want [photos]", got)
	}
	if got := extractStringSlice(parsed.Properties, "photo"); len(got) != 2 || got[0] != "https://i.example/one.jpg" || got[1] != "https://i.example/two.jpg" {
		t.Fatalf("photos = %#v", got)
	}
	if len(uploaded) != 2 || uploaded[0] != "one.jpg:one" || uploaded[1] != "two.jpg:two" {
		t.Fatalf("uploaded = %#v", uploaded)
	}
}

func TestRewriteMultipartCreateRequestRejectsUnsupportedFileProperty(t *testing.T) {
	t.Parallel()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("h", "entry"); err != nil {
		t.Fatalf("WriteField h: %v", err)
	}

	part, err := writer.CreateFormFile("audio", "clip.mp3")
	if err != nil {
		t.Fatalf("CreateFormFile audio: %v", err)
	}
	if _, err := part.Write([]byte("audio")); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err = rewriteMultipartCreateRequest(httptest.NewRecorder(), req, allowAll, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
		return "", nil
	})
	if !errors.Is(err, micropub.ErrBadRequest) {
		t.Fatalf("rewriteMultipartCreateRequest error = %v, want ErrBadRequest", err)
	}
}

func TestMicropubConfigUsesEndpointURLForMediaEndpoint(t *testing.T) {
	t.Parallel()

	impl := &jekyllMicropub{
		siteURL:     "https://chenna.me",
		endpointURL: "https://micropub.chenna.me",
	}

	req := httptest.NewRequest(http.MethodGet, "/micropub?q=config", nil)
	rec := httptest.NewRecorder()

	newMicropubHandler(impl).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got := payload["media-endpoint"]; got != "https://micropub.chenna.me/media" {
		t.Fatalf("media-endpoint = %#v, want %q", got, "https://micropub.chenna.me/media")
	}
}

// newMultipartPhotoCreate builds a multipart h=entry create with one photo.
// extraFields are written as plain form fields (e.g. access_token).
func newMultipartPhotoCreate(t *testing.T, extraFields map[string]string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("h", "entry"); err != nil {
		t.Fatalf("WriteField h: %v", err)
	}
	for key, value := range extraFields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField %s: %v", key, err)
		}
	}
	part, err := writer.CreateFormFile("photo", "one.jpg")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("one")); err != nil {
		t.Fatalf("write photo: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

// newCountingTokenServer returns a token endpoint that grants scope to any
// token for https://chenna.me, and a counter of how often it was called.
func newCountingTokenServer(t *testing.T, scope string) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"me":        "https://chenna.me/",
			"scope":     scope,
			"client_id": "https://client.example/",
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestRewriteMultipartCreateRequestAuthorizesBeforeUpload(t *testing.T) {
	t.Parallel()

	req := newMultipartPhotoCreate(t, nil)
	uploads := 0
	_, err := rewriteMultipartCreateRequest(httptest.NewRecorder(), req, func(*http.Request) error {
		return errUnauthorized
	}, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
		uploads++
		return "https://i.example/" + header.Filename, nil
	})
	if !errors.Is(err, errUnauthorized) {
		t.Fatalf("error = %v, want errUnauthorized", err)
	}
	if uploads != 0 {
		t.Fatalf("upload called %d times for an unauthorized request", uploads)
	}
}

func TestMicropubHandlerMultipartCreateRejectsUnauthorizedBeforeUpload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		scope         string
		header        string
		fields        map[string]string
		wantStatus    int
		wantError     string
		wantTokenHits int64
	}{
		{
			name:          "no token",
			scope:         "create",
			wantStatus:    http.StatusUnauthorized,
			wantError:     "unauthorized",
			wantTokenHits: 0,
		},
		{
			name:          "non-bearer authorization header",
			scope:         "create",
			header:        "Basic Zm9vOmJhcg==",
			wantStatus:    http.StatusUnauthorized,
			wantError:     "unauthorized",
			wantTokenHits: 0,
		},
		{
			name:          "header token without create scope",
			scope:         "update media",
			header:        "Bearer token-123",
			wantStatus:    http.StatusForbidden,
			wantError:     "insufficient_scope",
			wantTokenHits: 1,
		},
		{
			name:          "form token without create scope",
			scope:         "update",
			fields:        map[string]string{"access_token": "token-123"},
			wantStatus:    http.StatusForbidden,
			wantError:     "insufficient_scope",
			wantTokenHits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tokenServer, tokenHits := newCountingTokenServer(t, tt.scope)
			impl := &jekyllMicropub{
				siteURL:       "https://chenna.me",
				endpointURL:   "https://micropub.chenna.me",
				tokenEndpoint: tokenServer.URL,
			}

			var uploads atomic.Int64
			handler := newMicropubHandlerWithUpload(impl, multipartCreateAuthorizer(impl.HasScope), func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
				uploads.Add(1)
				return "https://i.example/" + header.Filename, nil
			})

			req := newMultipartPhotoCreate(t, tt.fields)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal(%q): %v", rec.Body.String(), err)
			}
			if payload["error"] != tt.wantError {
				t.Fatalf("error = %q, want %q", payload["error"], tt.wantError)
			}
			if got := uploads.Load(); got != 0 {
				t.Fatalf("upload called %d times for an unauthorized request", got)
			}
			if got := tokenHits.Load(); got != tt.wantTokenHits {
				t.Fatalf("token endpoint hits = %d, want %d", got, tt.wantTokenHits)
			}
		})
	}
}

func TestMicropubHandlerMultipartCreateUploadsAfterAuthorization(t *testing.T) {
	t.Parallel()

	tokenServer, tokenHits := newCountingTokenServer(t, "create")
	impl := &jekyllMicropub{
		siteURL:       "https://chenna.me",
		endpointURL:   "https://micropub.chenna.me",
		tokenEndpoint: tokenServer.URL,
	}

	var uploads atomic.Int64
	handler := newMicropubHandlerWithUpload(impl, multipartCreateAuthorizer(impl.HasScope), func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
		uploads.Add(1)
		// Stop before Create so the test needs no git repository.
		return "", errors.New("stop after upload")
	})

	req := newMultipartPhotoCreate(t, map[string]string{"access_token": "token-123"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if got := uploads.Load(); got != 1 {
		t.Fatalf("upload called %d times, want 1", got)
	}
	if got := tokenHits.Load(); got != 1 {
		t.Fatalf("token endpoint hits = %d, want 1", got)
	}
}

func TestRewriteMultipartCreateRequestMarksCreatePreauthorized(t *testing.T) {
	t.Parallel()

	tokenServer, tokenHits := newCountingTokenServer(t, "create")
	router := micropubRouter{&jekyllMicropub{
		siteURL:       "https://chenna.me",
		tokenEndpoint: tokenServer.URL,
	}}

	rewritten, err := rewriteMultipartCreateRequest(httptest.NewRecorder(), newMultipartPhotoCreate(t, nil), allowAll, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
		return "https://i.example/" + header.Filename, nil
	})
	if err != nil {
		t.Fatalf("rewriteMultipartCreateRequest: %v", err)
	}

	if !router.HasScope(rewritten, "create") {
		t.Fatal("HasScope(create) = false for a preauthorized request")
	}
	if got := tokenHits.Load(); got != 0 {
		t.Fatalf("token endpoint hits = %d, want 0 for a preauthorized scope", got)
	}

	// Other scopes and requests without the marker still go to the token endpoint.
	if router.HasScope(rewritten, "delete") {
		t.Fatal("HasScope(delete) = true, want false")
	}
	plain := httptest.NewRequest(http.MethodPost, "/micropub", nil)
	if router.HasScope(plain, "create") {
		t.Fatal("HasScope(create) = true for a request with no token")
	}
}
