package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registered for image.DecodeConfig (also imported by imaging)
	"image/jpeg"
	_ "image/png" // registered for image.DecodeConfig (also imported by imaging)
	"io"
	"log"
	"strings"
	"time"

	"github.com/chai2010/webp" // also registers the "webp" format with package image
	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"go.n16f.net/thumbhash"
)

// Limits checked from the image header before decoding, so a small file that
// declares enormous dimensions (a decompression bomb) is rejected without
// allocating its pixel buffer. Current phone sensors top out around 48-50 MP
// in full-resolution mode (e.g. 8064x6048); 64 MP leaves headroom while
// bounding a decoded NRGBA buffer to ~256 MiB.
const (
	maxImagePixels = 64_000_000
	maxImageSide   = 20_000 // generous for panoramas; still bounded by maxImagePixels
)

var errImageTooLarge = errors.New("image dimensions exceed limit")

// checkImageDimensions reads only the image header and rejects images whose
// declared dimensions are unreasonable.
func checkImageDimensions(data []byte) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("reading image header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid %s image dimensions %dx%d", format, cfg.Width, cfg.Height)
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return fmt.Errorf("%w: %s image is %dx%d (max %d px per side, %d px total)",
			errImageTooLarge, format, cfg.Width, cfg.Height, maxImageSide, maxImagePixels)
	}
	return nil
}

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

// EXIF 2.31 offset tags. goexif does not know them, so its built-in parser
// drops them; exifOffsetParser loads them from the EXIF sub-IFD.
const (
	exifOffsetTime         exif.FieldName = "OffsetTime"         // offset for DateTime
	exifOffsetTimeOriginal exif.FieldName = "OffsetTimeOriginal" // offset for DateTimeOriginal
)

var exifOffsetFields = map[uint16]exif.FieldName{
	0x9010: exifOffsetTime,
	0x9011: exifOffsetTimeOriginal,
}

type exifOffsetParser struct{}

func (exifOffsetParser) Parse(x *exif.Exif) error {
	ptr, err := x.Get(exif.ExifIFDPointer)
	if err != nil {
		return nil // no EXIF sub-IFD
	}
	offset, err := ptr.Int64(0)
	if err != nil {
		return nil
	}
	r := bytes.NewReader(x.Raw)
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	dir, _, err := tiff.DecodeDir(r, x.Tiff.Order)
	if err != nil {
		return nil // the built-in parser already reports sub-IFD errors
	}
	x.LoadTags(dir, exifOffsetFields, false)
	return nil
}

func init() {
	exif.RegisterParsers(exifOffsetParser{})
}

// extractExifDate attempts to read the DateTimeOriginal (or DateTime) from raw
// image bytes. Returns nil if the data has no usable EXIF date.
//
// EXIF date/time values are wall-clock times without a zone. When the file
// carries an OffsetTimeOriginal/OffsetTime tag (most phones since ~2018), the
// wall clock is interpreted in that offset. Otherwise goexif's behaviour is
// kept: a Canon TimeInfo zone if present, else the server's time.Local. On a
// UTC server that means offset-less photos are treated as UTC wall-clock
// time; set TZ to the photographer's home zone to change the assumption.
func extractExifDate(data []byte) *time.Time {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	t, err := x.DateTime()
	if err != nil || t.IsZero() {
		return nil
	}
	if loc := exifOffsetLocation(x); loc != nil {
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	}
	log.Printf("exif date extracted: %s", t.Format(time.RFC3339))
	return &t
}

// exifOffsetLocation returns the zone from the EXIF offset tag that matches
// the date x.DateTime() used, or nil if there is none or it is malformed.
func exifOffsetLocation(x *exif.Exif) *time.Location {
	name := exifOffsetTimeOriginal
	if _, err := x.Get(exif.DateTimeOriginal); err != nil {
		name = exifOffsetTime
	}
	tag, err := x.Get(name)
	if err != nil || tag.Format() != tiff.StringVal {
		return nil
	}
	raw := strings.TrimSpace(strings.TrimRight(string(tag.Val), "\x00"))
	offset, err := time.Parse("-07:00", raw)
	if err != nil {
		return nil
	}
	_, seconds := offset.Zone()
	return time.FixedZone("", seconds)
}

// processImage reads an image, auto-orients it, and produces responsive variants.
func processImage(r io.Reader) (*resizeResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading image: %w", err)
	}

	if err := checkImageDimensions(data); err != nil {
		return nil, err
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
