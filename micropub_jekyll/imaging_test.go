package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

// pngHeaderOnly returns a PNG signature plus a valid IHDR chunk declaring the
// given dimensions and no pixel data: a few dozen bytes that would make a
// naive decoder allocate width*height*4 bytes.
func pngHeaderOnly(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8  // bit depth
	ihdr[9] = 6  // color type: RGBA
	ihdr[10] = 0 // compression
	ihdr[11] = 0 // filter
	ihdr[12] = 0 // interlace

	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(chunk)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return buf.Bytes()
}

func TestProcessImageRejectsDecompressionBomb(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		width, height uint32
	}{
		{name: "huge area", width: 30000, height: 30000},
		{name: "over pixel cap", width: 10000, height: 6401},
		{name: "absurd side", width: maxImageSide + 1, height: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := pngHeaderOnly(tt.width, tt.height)
			if len(data) > 64 {
				t.Fatalf("crafted PNG is %d bytes, expected a tiny header", len(data))
			}
			_, err := processImage(bytes.NewReader(data))
			if !errors.Is(err, errImageTooLarge) {
				t.Fatalf("processImage error = %v, want errImageTooLarge", err)
			}
		})
	}
}

func TestCheckImageDimensionsAllowsLargePhonePhoto(t *testing.T) {
	t.Parallel()

	// 48 MP phone sensor, full resolution.
	if err := checkImageDimensions(pngHeaderOnly(8064, 6048)); err != nil {
		t.Fatalf("checkImageDimensions(8064x6048) = %v, want nil", err)
	}
}

func TestProcessImageSmallPNG(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			src.Set(x, y, color.NRGBA{R: uint8(x * 20), G: uint8(y * 30), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	result, err := processImage(&buf)
	if err != nil {
		t.Fatalf("processImage: %v", err)
	}
	if result.Width != 12 || result.Height != 8 {
		t.Fatalf("dimensions = %dx%d, want 12x8", result.Width, result.Height)
	}
	if len(result.Variants) != len(variants) || len(result.WebPVariants) != len(variants) {
		t.Fatalf("variants = %d JPEG / %d WebP, want %d each", len(result.Variants), len(result.WebPVariants), len(variants))
	}
	if result.ThumbHash == "" {
		t.Fatal("ThumbHash is empty")
	}
}

type tiffEntry struct {
	tag   uint16
	value string // ASCII value, NUL terminator added automatically
}

// tiffWithExifIFD builds a little-endian TIFF/EXIF blob whose IFD0 points to
// an EXIF sub-IFD containing the given ASCII entries.
func tiffWithExifIFD(entries []tiffEntry) []byte {
	le := binary.LittleEndian
	const ifd0Offset = 8
	const ifd0Size = 2 + 12 + 4
	subIFDOffset := ifd0Offset + ifd0Size
	subIFDSize := 2 + 12*len(entries) + 4
	dataOffset := subIFDOffset + subIFDSize

	var head, data bytes.Buffer
	head.WriteString("II")
	_ = binary.Write(&head, le, uint16(42))
	_ = binary.Write(&head, le, uint32(ifd0Offset))

	// IFD0: a single ExifIFDPointer (LONG).
	_ = binary.Write(&head, le, uint16(1))
	_ = binary.Write(&head, le, uint16(0x8769))
	_ = binary.Write(&head, le, uint16(4))
	_ = binary.Write(&head, le, uint32(1))
	_ = binary.Write(&head, le, uint32(subIFDOffset))
	_ = binary.Write(&head, le, uint32(0))

	// EXIF sub-IFD with out-of-line ASCII values.
	_ = binary.Write(&head, le, uint16(len(entries)))
	for _, e := range entries {
		value := append([]byte(e.value), 0)
		_ = binary.Write(&head, le, e.tag)
		_ = binary.Write(&head, le, uint16(2)) // ASCII
		_ = binary.Write(&head, le, uint32(len(value)))
		if len(value) <= 4 {
			padded := make([]byte, 4)
			copy(padded, value)
			head.Write(padded)
		} else {
			_ = binary.Write(&head, le, uint32(dataOffset+data.Len()))
			data.Write(value)
		}
	}
	_ = binary.Write(&head, le, uint32(0))

	return append(head.Bytes(), data.Bytes()...)
}

func TestExtractExifDateUsesOffsetTimeOriginal(t *testing.T) {
	t.Parallel()

	data := tiffWithExifIFD([]tiffEntry{
		{tag: 0x9003, value: "2026:04:03 08:00:00"}, // DateTimeOriginal
		{tag: 0x9011, value: "+05:30"},              // OffsetTimeOriginal
	})

	got := extractExifDate(data)
	if got == nil {
		t.Fatal("extractExifDate = nil")
	}
	want := time.Date(2026, 4, 3, 2, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("extractExifDate = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if _, offset := got.Zone(); offset != 5*3600+30*60 {
		t.Fatalf("zone offset = %d, want %d", offset, 5*3600+30*60)
	}
}

func TestExtractExifDateWithoutOffsetUsesLocal(t *testing.T) {
	t.Parallel()

	data := tiffWithExifIFD([]tiffEntry{
		{tag: 0x9003, value: "2026:04:03 08:00:00"},
	})

	got := extractExifDate(data)
	if got == nil {
		t.Fatal("extractExifDate = nil")
	}
	want := time.Date(2026, 4, 3, 8, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("extractExifDate = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestExtractExifDateIgnoresMalformedOffset(t *testing.T) {
	t.Parallel()

	data := tiffWithExifIFD([]tiffEntry{
		{tag: 0x9003, value: "2026:04:03 08:00:00"},
		{tag: 0x9011, value: "   :  "},
	})

	got := extractExifDate(data)
	if got == nil {
		t.Fatal("extractExifDate = nil")
	}
	want := time.Date(2026, 4, 3, 8, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("extractExifDate = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
