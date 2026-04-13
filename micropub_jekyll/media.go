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
	handler := micropub.NewMediaHandler(impl.uploadMediaWithoutRequestContext, impl.hasScope)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		handler.ServeHTTP(w, r)
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	})
}

func (j *jekyllMicropub) uploadMediaWithoutRequestContext(file multipart.File, header *multipart.FileHeader) (string, error) {
	return j.uploadMedia(context.Background(), file, header)
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

	// Generate unique base name, preferring the image's EXIF date over wall clock.
	counter := mediaCounter.Add(1)
	baseTime := time.Now().UTC()
	if result.ExifDate != nil {
		baseTime = result.ExifDate.UTC()
	}
	baseName := fmt.Sprintf("%s-%d", baseTime.Format("2006-01-02-150405"), counter)

	// Upload all variants to GCS
	ctx, cancel := context.WithTimeout(ctx, mediaUploadTimeout)
	defer cancel()
	if err := j.gcs.uploadVariants(ctx, baseName, result); err != nil {
		return "", fmt.Errorf("uploading to GCS: %w", err)
	}

	// Return a concrete uploaded object so the Location header is dereferenceable.
	cdnURL := mediaObjectURL(j.imageBaseURL, baseName)

	// Cache the ThumbHash keyed by the CDN URL so Create() can embed it.
	if result.ThumbHash != "" {
		j.thumbhashCache.Store(cdnURL, result.ThumbHash)
	}

	// Cache the EXIF date keyed by the CDN URL so Create() can use it as the post date.
	if result.ExifDate != nil {
		j.exifDateCache.Store(cdnURL, *result.ExifDate)
	}

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
