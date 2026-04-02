package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testEnv struct {
	db     *sql.DB
	server http.Handler
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	db, err := initDB(filepath.Join(t.TempDir(), "guestbook.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	return testEnv{
		db:     db,
		server: newServer(db, "test-token", newRateLimiter(1000, time.Minute)),
	}
}

func (env testEnv) request(t *testing.T, method, target string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Host = "guestbook.test"
	request.Header.Set("X-Forwarded-Proto", "https")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	env.server.ServeHTTP(recorder, request)
	return recorder
}

func TestHandleCreateEntryNormalizesMessageEntries(t *testing.T) {
	env := newTestEnv(t)

	payload := []byte(`{"name":" Alice ","website":"example.com","entry_type":"text","content":"  hi\n  there  "}`)
	recorder := env.request(t, http.MethodPost, "/api/entries", payload, map[string]string{
		"Content-Type": "application/json",
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	entries, err := getPendingEntries(env.db)
	if err != nil {
		t.Fatalf("get pending entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 pending entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Name != "Alice" {
		t.Fatalf("expected trimmed name, got %q", entry.Name)
	}
	if entry.Website != "https://example.com" {
		t.Fatalf("expected normalized website, got %q", entry.Website)
	}
	if entry.EntryType != "message" {
		t.Fatalf("expected entry_type message, got %q", entry.EntryType)
	}
	if entry.Content != "  hi\n  there  " {
		t.Fatalf("expected preserved content whitespace, got %q", entry.Content)
	}
}

func TestHandleCreateEntryRejectsUnsupportedWebsiteScheme(t *testing.T) {
	env := newTestEnv(t)

	payload := []byte(`{"name":"Alice","website":"ftp://example.com","entry_type":"message","content":"Hello"}`)
	recorder := env.request(t, http.MethodPost, "/api/entries", payload, map[string]string{
		"Content-Type": "application/json",
	})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(response["error"], "http or https") {
		t.Fatalf("expected website validation error, got %q", response["error"])
	}
}

func TestHandleGetEntriesReturnsPaginatedResponses(t *testing.T) {
	env := newTestEnv(t)

	messageID, err := createEntry(env.db, &Entry{
		Name:      "Older entry",
		EntryType: "message",
		Content:   "One",
	})
	if err != nil {
		t.Fatalf("create message entry: %v", err)
	}
	if err := approveEntry(env.db, messageID); err != nil {
		t.Fatalf("approve message entry: %v", err)
	}

	drawingID, err := createEntry(env.db, &Entry{
		Name:      "Newer entry",
		EntryType: "drawing",
		ImageData: []byte("fake-png-data"),
	})
	if err != nil {
		t.Fatalf("create drawing entry: %v", err)
	}
	if err := approveEntry(env.db, drawingID); err != nil {
		t.Fatalf("approve drawing entry: %v", err)
	}

	if _, err := env.db.Exec(`UPDATE entries SET created_at = ? WHERE id = ?`, "2026-04-02 10:00:00", messageID); err != nil {
		t.Fatalf("set message timestamp: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE entries SET created_at = ? WHERE id = ?`, "2026-04-02 11:00:00", drawingID); err != nil {
		t.Fatalf("set drawing timestamp: %v", err)
	}

	recorder := env.request(t, http.MethodGet, "/api/entries?page=1&per_page=1", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response entryListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode paginated response: %v", err)
	}

	if len(response.Entries) != 1 {
		t.Fatalf("expected 1 entry on page 1, got %d", len(response.Entries))
	}
	if response.Entries[0].ID != drawingID {
		t.Fatalf("expected newest drawing entry on page 1, got %d", response.Entries[0].ID)
	}
	if !strings.HasSuffix(response.Entries[0].ImageURL, fmt.Sprintf("/api/entries/%d/image", drawingID)) {
		t.Fatalf("expected public image url for drawing, got %q", response.Entries[0].ImageURL)
	}
	if response.Pagination.TotalEntries != 2 || response.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected pagination totals: %+v", response.Pagination)
	}
	if !response.Pagination.HasNext || response.Pagination.NextPage != 2 {
		t.Fatalf("expected next page metadata, got %+v", response.Pagination)
	}

	recorder = env.request(t, http.MethodGet, "/api/entries?page=2&per_page=1", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d on page 2, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode page 2 response: %v", err)
	}
	if len(response.Entries) != 1 || response.Entries[0].ID != messageID {
		t.Fatalf("expected older message entry on page 2, got %+v", response.Entries)
	}
}

func TestImageRoutesHidePendingEntriesFromPublic(t *testing.T) {
	env := newTestEnv(t)
	imageData := []byte("fake-image-data")

	entryID, err := createEntry(env.db, &Entry{
		Name:      "Sketch",
		EntryType: "drawing",
		ImageData: imageData,
	})
	if err != nil {
		t.Fatalf("create drawing entry: %v", err)
	}

	publicRecorder := env.request(t, http.MethodGet, fmt.Sprintf("/api/entries/%d/image", entryID), nil, nil)
	if publicRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected pending public image to be hidden, got %d", publicRecorder.Code)
	}

	adminRecorder := env.request(t, http.MethodGet, fmt.Sprintf("/api/admin/entries/%d/image", entryID), nil, map[string]string{
		"Authorization": "Bearer test-token",
	})
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("expected admin image access, got %d: %s", adminRecorder.Code, adminRecorder.Body.String())
	}
	if !bytes.Equal(adminRecorder.Body.Bytes(), imageData) {
		t.Fatalf("expected admin image bytes to match original data")
	}

	if err := approveEntry(env.db, entryID); err != nil {
		t.Fatalf("approve drawing entry: %v", err)
	}

	publicRecorder = env.request(t, http.MethodGet, fmt.Sprintf("/api/entries/%d/image", entryID), nil, nil)
	if publicRecorder.Code != http.StatusOK {
		t.Fatalf("expected approved public image, got %d: %s", publicRecorder.Code, publicRecorder.Body.String())
	}
	if !bytes.Equal(publicRecorder.Body.Bytes(), imageData) {
		t.Fatalf("expected public image bytes to match original data")
	}
}

func TestRequireAdminRejectsInvalidTokens(t *testing.T) {
	env := newTestEnv(t)

	// No Authorization header
	recorder := env.request(t, http.MethodGet, "/api/admin/entries", nil, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no auth header, got %d", recorder.Code)
	}

	// Wrong token
	recorder = env.request(t, http.MethodGet, "/api/admin/entries", nil, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", recorder.Code)
	}

	// Valid token
	recorder = env.request(t, http.MethodGet, "/api/admin/entries", nil, map[string]string{
		"Authorization": "Bearer test-token",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleCreateEntryValidation(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "empty name",
			payload: `{"name":"","entry_type":"message","content":"hello"}`,
			wantErr: "name is required",
		},
		{
			name:    "missing entry type",
			payload: `{"name":"Alice","entry_type":"","content":"hello"}`,
			wantErr: "entry_type must be",
		},
		{
			name:    "message without content",
			payload: `{"name":"Alice","entry_type":"message","content":""}`,
			wantErr: "content is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := env.request(t, http.MethodPost, "/api/entries", []byte(tt.payload), map[string]string{
				"Content-Type": "application/json",
			})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
			var response map[string]string
			json.Unmarshal(recorder.Body.Bytes(), &response)
			if !strings.Contains(response["error"], tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, response["error"])
			}
		})
	}
}

func TestDeleteAndPurgeAdminEndpoints(t *testing.T) {
	env := newTestEnv(t)
	adminHeaders := map[string]string{"Authorization": "Bearer test-token"}

	// Create an entry
	id, err := createEntry(env.db, &Entry{
		Name: "ToDelete", EntryType: "message", Content: "bye",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	// Delete the entry
	recorder := env.request(t, http.MethodDelete, fmt.Sprintf("/api/admin/entries/%d", id), nil, adminHeaders)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Delete again should 404
	recorder = env.request(t, http.MethodDelete, fmt.Sprintf("/api/admin/entries/%d", id), nil, adminHeaders)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on re-delete, got %d", recorder.Code)
	}

	// Create entries and reject them for purge test
	for i := 0; i < 3; i++ {
		rid, _ := createEntry(env.db, &Entry{
			Name: fmt.Sprintf("Rejected%d", i), EntryType: "message", Content: "spam",
		})
		rejectEntry(env.db, rid)
	}

	recorder = env.request(t, http.MethodPost, "/api/admin/purge-rejected", nil, adminHeaders)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 on purge, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var purgeResp map[string]any
	json.Unmarshal(recorder.Body.Bytes(), &purgeResp)
	if purgeResp["deleted"] != float64(3) {
		t.Fatalf("expected 3 deleted, got %v", purgeResp["deleted"])
	}
}
