package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"time"
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
		go t.sendPhoto(entry.ImageData, caption)
	} else {
		msg := fmt.Sprintf("New message in guestbook!\n\nFrom: %s\n\n%s", entry.Name, entry.Content)
		if entry.Website != "" {
			msg += "\n\nWebsite: " + entry.Website
		}
		go t.sendMessage(msg)
	}
}

func (t *telegramNotifier) sendMessage(text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	body, err := json.Marshal(map[string]string{
		"chat_id": t.chatID,
		"text":    text,
	})
	if err != nil {
		log.Printf("telegram: failed to marshal message: %v", err)
		return
	}

	resp, err := t.client.Post(url, "application/json", bytes.NewReader(body))
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

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", t.token)
	resp, err := t.client.Post(url, w.FormDataContentType(), &buf)
	if err != nil {
		log.Printf("telegram: failed to send photo: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("telegram: sendPhoto unexpected status %d", resp.StatusCode)
	}
}
