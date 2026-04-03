package main

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.hacdias.com/indielib/micropub"
)

// mediaCounter ensures unique names when multiple images are uploaded in the same second.
var mediaCounter atomic.Int64

const mediaURLSuffix = "-xlarge.jpg"
const mediaUploadTimeout = 30 * time.Second

func newMediaHandler(impl *jekyllMicropub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		if !impl.hasScope(r, "media") {
			http.Error(w, "insufficient scope", http.StatusForbidden)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, micropub.DefaultMaxMediaSize)
		if err := r.ParseMultipartForm(0); err != nil {
			http.Error(w, "invalid media upload", http.StatusBadRequest)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing media file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		location, err := impl.uploadMedia(r.Context(), file, header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusCreated)
	})
}

// uploadMedia handles media uploads: resize and push to GCS.
func (j *jekyllMicropub) uploadMedia(ctx context.Context, file multipart.File, header *multipart.FileHeader) (string, error) {
	if j.gcs == nil {
		return "", fmt.Errorf("media uploads are not configured (GCS_BUCKET not set)")
	}

	log.Printf("media upload: %s (%d bytes)", header.Filename, header.Size)

	// Process image into responsive variants
	result, err := processImage(file)
	if err != nil {
		return "", fmt.Errorf("processing image: %w", err)
	}

	// Generate unique base name
	counter := mediaCounter.Add(1)
	baseName := fmt.Sprintf("%s-%d", generateBaseName(), counter)

	// Upload all variants to GCS
	ctx, cancel := context.WithTimeout(ctx, mediaUploadTimeout)
	defer cancel()
	if err := j.gcs.uploadVariants(ctx, baseName, result); err != nil {
		return "", fmt.Errorf("uploading to GCS: %w", err)
	}

	// Return a concrete uploaded object so the Location header is dereferenceable.
	cdnURL := mediaObjectURL(j.imageBaseURL, baseName)
	log.Printf("media uploaded: %s -> %s", header.Filename, cdnURL)
	return cdnURL, nil
}

func mediaObjectURL(baseURL, baseName string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasPrefix(baseURL, "//") {
		baseURL = "https:" + baseURL
	}
	return fmt.Sprintf("%s/%s%s", baseURL, baseName, mediaURLSuffix)
}
