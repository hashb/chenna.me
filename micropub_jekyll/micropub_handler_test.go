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
	"testing"

	"go.hacdias.com/indielib/micropub"
)

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
	rewritten, err := rewriteMultipartCreateRequest(httptest.NewRecorder(), req, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
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

	_, err = rewriteMultipartCreateRequest(httptest.NewRecorder(), req, func(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
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
