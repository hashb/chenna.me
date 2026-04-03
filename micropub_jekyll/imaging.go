package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	"github.com/disintegration/imaging"
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
	Variants map[string][]byte // suffix -> JPEG bytes
	Width    int               // original width
	Height   int               // original height
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
		Variants: make(map[string][]byte, len(variants)),
		Width:    origWidth,
		Height:   origHeight,
	}

	for _, v := range variants {
		var resized image.Image
		if v.Width >= origWidth {
			// Don't upscale — use original
			resized = src
		} else {
			resized = imaging.Resize(src, v.Width, 0, imaging.Lanczos)
		}

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return nil, fmt.Errorf("encoding %s variant: %w", v.Suffix, err)
		}
		result.Variants[v.Suffix] = buf.Bytes()
	}

	return result, nil
}
