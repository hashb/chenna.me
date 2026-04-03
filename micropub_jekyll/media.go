package main

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"sync/atomic"
)

// mediaCounter ensures unique names when multiple images are uploaded in the same second.
var mediaCounter atomic.Int64

// uploadMedia handles media uploads: resize and push to GCS.
func (j *jekyllMicropub) uploadMedia(file multipart.File, header *multipart.FileHeader) (string, error) {
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
	ctx := context.Background()
	if err := j.gcs.uploadVariants(ctx, baseName, result); err != nil {
		return "", fmt.Errorf("uploading to GCS: %w", err)
	}

	// Return the CDN URL (without size suffix — the responsive_image include handles that)
	cdnURL := fmt.Sprintf("https:%s/%s", j.imageBaseURL, baseName)
	log.Printf("media uploaded: %s -> %s", header.Filename, cdnURL)
	return cdnURL, nil
}
