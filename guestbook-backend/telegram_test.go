package main

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func TestTelegramErrorsDoNotLogToken(t *testing.T) {
	const token = "123456:SECRET-TOKEN"
	notifier := newTelegramNotifier(token, "42")
	notifier.client.Transport = failingTransport{}

	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	notifier.sendMessage("hi")
	notifier.sendPhoto([]byte("png"), "caption")

	out := logs.String()
	if strings.Contains(out, "SECRET") {
		t.Fatalf("token leaked into logs: %s", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("expected underlying error in logs, got: %s", out)
	}
}

func TestTruncateUTF16(t *testing.T) {
	if got := truncateUTF16("short", 10); got != "short" {
		t.Fatalf("expected unchanged string, got %q", got)
	}

	long := strings.Repeat("a", 1000) + strings.Repeat("😀", 100)
	got := truncateUTF16(long, maxTelegramCaption)
	if n := len(utf16.Encode([]rune(got))); n > maxTelegramCaption {
		t.Fatalf("expected at most %d UTF-16 units, got %d", maxTelegramCaption, n)
	}
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, strings.Repeat("a", 1000)) {
		t.Fatalf("unexpected truncation result %q", got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncation split a rune: %q", got)
	}
}
