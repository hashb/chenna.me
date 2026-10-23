package main

import (
	"bytes"
	"context"
	"fmt"

	"cloud.google.com/go/storage"
)

type gcsUploader struct {
	client *storage.Client
	bucket string
	prefix string
}

// upload uploads image bytes to GCS. Objects are immutable once published
// (served with a one-year cache), so the write fails rather than overwriting
// an existing object with the same name.
func (g *gcsUploader) upload(ctx context.Context, objectName string, data []byte, contentType string) error {
	obj := g.client.Bucket(g.bucket).Object(objectName).If(storage.Conditions{DoesNotExist: true})
	w := obj.NewWriter(ctx)
	w.ContentType = contentType
	w.CacheControl = "public, max-age=31536000"

	if _, err := bytes.NewReader(data).WriteTo(w); err != nil {
		w.Close()
		return fmt.Errorf("writing to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing GCS writer: %w", err)
	}
	return nil
}

// uploadVariants uploads all responsive image variants (JPEG and WebP) to GCS.
// Returns the base name (without suffix) used for the responsive_image include.
func (g *gcsUploader) uploadVariants(ctx context.Context, baseName string, result *resizeResult) error {
	for suffix, data := range result.Variants {
		objectName := fmt.Sprintf("%s/%s%s.jpg", g.prefix, baseName, suffix)
		if err := g.upload(ctx, objectName, data, "image/jpeg"); err != nil {
			return fmt.Errorf("uploading %s: %w", objectName, err)
		}
	}
	for suffix, data := range result.WebPVariants {
		objectName := fmt.Sprintf("%s/%s%s.webp", g.prefix, baseName, suffix)
		if err := g.upload(ctx, objectName, data, "image/webp"); err != nil {
			return fmt.Errorf("uploading %s: %w", objectName, err)
		}
	}
	return nil
}
