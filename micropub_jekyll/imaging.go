package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"time"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
	"go.n16f.net/thumbhash"
)

// imageVariant describes a responsive image variant.
type imageVariant struct {
	Suffix string
	Width  int
}

var variants = []imageVariant{
	{Suffix: "-small", Width: 320},
	{Suffix: "-medium", Width: 640},
	{Suffix: "-large", Width: 1024},
	{Suffix: "-xlarge", Width: 1920},
}

// resizeResult holds the resized image bytes for each variant.
type resizeResult struct {
	Variants     map[string][]byte // suffix -> JPEG bytes
	WebPVariants map[string][]byte // suffix -> WebP bytes
	Width        int               // original width
	Height       int               // original height
	ThumbHash    string            // base64-encoded ThumbHash for blur-up placeholder
	ExifDate     *time.Time        // DateTimeOriginal from EXIF, nil if not present
}

// extractExifDate attempts to read the DateTimeOriginal (or DateTime) from raw
// image bytes. Returns nil if the data has no usable EXIF date.
func extractExifDate(data []byte) *time.Time {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	t, err := x.DateTime()
	if err != nil || t.IsZero() {
		return nil
	}
	log.Printf("exif date extracted: %s", t.Format(time.RFC3339))
	return &t
}

// processImage reads an image, auto-orients it, and produces responsive variants.
func processImage(r io.Reader) (*resizeResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading image: %w", err)
	}

	// Decode with auto-orientation (imaging handles EXIF orientation)
	src, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	bounds := src.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	result := &resizeResult{
		Variants:     make(map[string][]byte, len(variants)),
		WebPVariants: make(map[string][]byte, len(variants)),
		Width:        origWidth,
		Height:       origHeight,
		ExifDate:     extractExifDate(data),
	}

	// Generate ThumbHash from a ≤100px thumbnail for blur-up placeholder
	thumb := imaging.Fit(src, 100, 100, imaging.Lanczos)
	hashBytes := thumbhash.EncodeImage(thumb)
	result.ThumbHash = base64.StdEncoding.EncodeToString(hashBytes)

	for _, v := range variants {
		var resized image.Image
		if v.Width >= origWidth {
			// Don't upscale — use original
			resized = src
		} else {
			resized = imaging.Resize(src, v.Width, 0, imaging.Lanczos)
		}

		var jpegBuf bytes.Buffer
		if err := jpeg.Encode(&jpegBuf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return nil, fmt.Errorf("encoding %s JPEG variant: %w", v.Suffix, err)
		}
		result.Variants[v.Suffix] = jpegBuf.Bytes()

		var webpBuf bytes.Buffer
		if err := webp.Encode(&webpBuf, resized, &webp.Options{Lossless: false, Quality: 85}); err != nil {
			return nil, fmt.Errorf("encoding %s WebP variant: %w", v.Suffix, err)
		}
		result.WebPVariants[v.Suffix] = webpBuf.Bytes()
	}

	return result, nil
}
