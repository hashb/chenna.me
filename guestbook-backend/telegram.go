package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"
)

// Telegram limits, counted in UTF-16 code units.
const (
	maxTelegramMessage = 4096
	maxTelegramCaption = 1024
)

type telegramNotifier struct {
	token  string
	chatID string
	client *http.Client
}

func newTelegramNotifier(token, chatID string) *telegramNotifier {
	return &telegramNotifier{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *telegramNotifier) notifyNewEntry(entry Entry) {
	if entry.EntryType == "drawing" {
		caption := fmt.Sprintf("New drawing in guestbook!\n\nFrom: %s", entry.Name)
		if entry.Website != "" {
			caption += "\n\nWebsite: " + entry.Website
		}
		go t.sendPhoto(entry.ImageData, truncateUTF16(caption, maxTelegramCaption))
	} else {
		msg := fmt.Sprintf("New message in guestbook!\n\nFrom: %s\n\n%s", entry.Name, entry.Content)
		if entry.Website != "" {
			msg += "\n\nWebsite: " + entry.Website
		}
		go t.sendMessage(truncateUTF16(msg, maxTelegramMessage))
	}
}

func (t *telegramNotifier) sendMessage(text string) {
	body, err := json.Marshal(map[string]string{
		"chat_id": t.chatID,
		"text":    text,
	})
	if err != nil {
		log.Printf("telegram: failed to marshal message: %v", err)
		return
	}

	resp, err := t.post("sendMessage", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("telegram: failed to send message: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("telegram: sendMessage unexpected status %d", resp.StatusCode)
	}
}

func (t *telegramNotifier) sendPhoto(imageData []byte, caption string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	_ = w.WriteField("chat_id", t.chatID)
	_ = w.WriteField("caption", caption)

	part, err := w.CreateFormFile("photo", "drawing.png")
	if err != nil {
		log.Printf("telegram: failed to create form file: %v", err)
		return
	}
	if _, err := part.Write(imageData); err != nil {
		log.Printf("telegram: failed to write image data: %v", err)
		return
	}
	w.Close()

	resp, err := t.post("sendPhoto", w.FormDataContentType(), &buf)
	if err != nil {
		log.Printf("telegram: failed to send photo: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("telegram: sendPhoto unexpected status %d", resp.StatusCode)
	}
}

// post calls a Bot API method. Errors never include the token-bearing URL.
func (t *telegramNotifier) post(method, contentType string, body io.Reader) (*http.Response, error) {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", t.token, method)
	resp, err := t.client.Post(endpoint, contentType, body)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		msg := err.Error()
		if t.token != "" {
			msg = strings.ReplaceAll(msg, t.token, "<redacted>")
		}
		return nil, fmt.Errorf("%s: %s", method, msg)
	}
	return resp, nil
}

// truncateUTF16 shortens s to at most limit UTF-16 code units without
// splitting a rune, appending an ellipsis when cut.
func truncateUTF16(s string, limit int) string {
	units := 0
	for _, r := range s {
		units += utf16.RuneLen(r)
	}
	if units <= limit {
		return s
	}
	const ellipsis = "…"
	limit -= utf16.RuneLen('…')
	units = 0
	for i, r := range s {
		n := utf16.RuneLen(r)
		if units+n > limit {
			return s[:i] + ellipsis
		}
		units += n
	}
	return s
}
