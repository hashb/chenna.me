package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewMediaBaseNameIsUniqueAndWellFormed(t *testing.T) {
	t.Parallel()

	// Same EXIF second, as with burst photos uploaded across restarts.
	ts := time.Date(2026, 4, 3, 8, 0, 0, 0, time.FixedZone("", 5*3600))
	pattern := regexp.MustCompile(`^2026-04-03-030000-[0-9a-f]{8}$`)

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		name, err := newMediaBaseName(ts)
		if err != nil {
			t.Fatalf("newMediaBaseName: %v", err)
		}
		if !pattern.MatchString(name) {
			t.Fatalf("newMediaBaseName = %q, want match for %s", name, pattern)
		}
		if seen[name] {
			t.Fatalf("newMediaBaseName returned duplicate %q", name)
		}
		seen[name] = true
	}
}

func TestNewMediaBaseNameRoundTripsThroughExtractBaseName(t *testing.T) {
	t.Parallel()

	const imageBaseURL = "//i.chenna.me/photos/prod/opt/micro"
	name, err := newMediaBaseName(time.Date(2026, 4, 3, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("newMediaBaseName: %v", err)
	}

	got := extractBaseName(mediaObjectURL(imageBaseURL, name), imageBaseURL)
	if !strings.HasSuffix(got, "/"+name) {
		t.Fatalf("extractBaseName(mediaObjectURL(%q)) = %q, want suffix %q", name, got, "/"+name)
	}
}
