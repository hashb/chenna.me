package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
	var msg string
	switch entry.EntryType {
	case "drawing":
		msg = fmt.Sprintf("New drawing in guestbook!\n\nFrom: %s", entry.Name)
	default:
		msg = fmt.Sprintf("New message in guestbook!\n\nFrom: %s\n\n%s", entry.Name, entry.Content)
	}
	if entry.Website != "" {
		msg += "\n\nWebsite: " + entry.Website
	}

	go t.send(msg)
}

func (t *telegramNotifier) send(text string) {
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
		log.Printf("telegram: unexpected status %d", resp.StatusCode)
	}
}
