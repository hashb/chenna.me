package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"go.hacdias.com/indielib/micropub"
)

const mediaURLSuffix = "-xlarge.jpg"
const mediaUploadTimeout = 30 * time.Second
const maxMediaSize int64 = 80 << 20 // 80 MiB

func newMediaHandler(impl *jekyllMicropub) http.Handler {
	handler := micropub.NewMediaHandler(impl.uploadMediaWithoutRequestContext, impl.hasScope, micropub.WithMaxMediaSize(maxMediaSize))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		// Token extraction may parse the form before indielib applies its own
		// limit. Bound that read too, and keep file parts on disk.
		r.Body = http.MaxBytesReader(w, r.Body, maxMediaSize)
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if err := r.ParseMultipartForm(0); err != nil {
				serveMicropubError(w, fmt.Errorf("%w: %w", micropub.ErrBadRequest, err))
				return
			}
		}
		handler.ServeHTTP(w, r)
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
	baseTime := time.Now().UTC()
	if result.ExifDate != nil {
		baseTime = result.ExifDate.UTC()
	}
	baseName, err := newMediaBaseName(baseTime)
	if err != nil {
		return "", err
	}

	// Upload all variants to GCS
	ctx, cancel := context.WithTimeout(ctx, mediaUploadTimeout)
	defer cancel()
	if err := j.gcs.uploadVariants(ctx, baseName, result); err != nil {
		return "", fmt.Errorf("uploading to GCS: %w", err)
	}

	// Return a concrete uploaded object so the Location header is dereferenceable.
	cdnURL := mediaObjectURL(j.imageBaseURL, baseName)

	// Cache the ThumbHash and EXIF date keyed by the CDN URL so Create() can
	// embed them; entries are timestamped so unused uploads get pruned.
	j.rememberMediaMetadata(cdnURL, result.ThumbHash, result.ExifDate)

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

// mediaNameSuffixBytes is the number of random bytes in a media base name.
// 4 bytes (8 hex chars) makes same-second collisions vanishingly unlikely;
// gcsUploader also writes with a DoesNotExist precondition as a backstop.
const mediaNameSuffixBytes = 4

// newMediaBaseName returns "<YYYY-MM-DD-HHMMSS>-<random hex>" for t. The
// random suffix keeps names unique across process restarts (burst photos
// often share an EXIF second), unlike an in-process counter. The suffix is
// pure hex, so it can never end in a variant suffix such as "-small" that
// extractBaseName strips.
func newMediaBaseName(t time.Time) (string, error) {
	var suffix [mediaNameSuffixBytes]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generating media name: %w", err)
	}
	return fmt.Sprintf("%s-%s", t.UTC().Format("2006-01-02-150405"), hex.EncodeToString(suffix[:])), nil
}
