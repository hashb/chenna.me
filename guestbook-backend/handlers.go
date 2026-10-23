package main

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultPublicPerPage = 24
	maxPublicPerPage     = 48
	maxNameRunes         = 80
	maxWebsiteLength     = 2048
	maxContentRunes      = 500
	maxJSONBodySize      = 64 << 10 // 64KB
	maxMultipartBodySize = 10 << 20 // 10MB
	maxMultipartMemory   = 1 << 20  // 1MB; larger parts spill to temp files
	maxImageSize         = 5 << 20  // 5MB
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type entryResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Website   string    `json:"website,omitempty"`
	EntryType string    `json:"entry_type"`
	Content   string    `json:"content,omitempty"`
	HasImage  bool      `json:"has_image"`
	ImageURL  string    `json:"image_url,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type entryListResponse struct {
	Entries    []entryResponse   `json:"entries"`
	Pagination paginationDetails `json:"pagination"`
}

type pendingEntryListResponse struct {
	Entries []entryResponse `json:"entries"`
}

type paginationDetails struct {
	Page         int  `json:"page"`
	PerPage      int  `json:"per_page"`
	TotalEntries int  `json:"total_entries"`
	TotalPages   int  `json:"total_pages"`
	HasPrevious  bool `json:"has_previous"`
	HasNext      bool `json:"has_next"`
	PreviousPage int  `json:"previous_page,omitempty"`
	NextPage     int  `json:"next_page,omitempty"`
}

type server struct {
	db         *sql.DB
	adminToken string
	limiter    *rateLimiter
	telegram   *telegramNotifier
	mux        *http.ServeMux
}

func newServer(db *sql.DB, adminToken string, limiter *rateLimiter, telegram *telegramNotifier) *server {
	s := &server{
		db:         db,
		adminToken: adminToken,
		limiter:    limiter,
		telegram:   telegram,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/entries", s.handleGetEntries)
	s.mux.HandleFunc("POST /api/entries", s.handleCreateEntry)
	s.mux.HandleFunc("GET /api/entries/{id}/image.png", s.handleGetImage)
	s.mux.HandleFunc("GET /api/admin/entries/{id}/image.png", s.requireAdmin(s.handleAdminGetImage))
	s.mux.HandleFunc("GET /api/admin/entries", s.requireAdmin(s.handleAdminGetEntries))
	s.mux.HandleFunc("POST /api/admin/entries/{id}/approve", s.requireAdmin(s.handleApproveEntry))
	s.mux.HandleFunc("POST /api/admin/entries/{id}/reject", s.requireAdmin(s.handleRejectEntry))
	s.mux.HandleFunc("DELETE /api/admin/entries/{id}", s.requireAdmin(s.handleDeleteEntry))
	s.mux.HandleFunc("POST /api/admin/purge-rejected", s.requireAdmin(s.handlePurgeRejected))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleGetEntries(w http.ResponseWriter, r *http.Request) {
	page := parsePositiveInt(r.URL.Query().Get("page"), 1, 1, 1000000)
	perPage := parsePositiveInt(r.URL.Query().Get("per_page"), defaultPublicPerPage, 1, maxPublicPerPage)

	pageData, err := getApprovedEntries(s.db, page, perPage)
	if err != nil {
		log.Printf("error getting entries: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	baseURL := requestBaseURL(r)
	responses := make([]entryResponse, 0, len(pageData.Entries))
	for _, entry := range pageData.Entries {
		responses = append(responses, buildEntryResponse(entry, baseURL, fmt.Sprintf("/api/entries/%d/image.png", entry.ID), false))
	}

	writeJSON(w, http.StatusOK, entryListResponse{
		Entries:    responses,
		Pagination: buildPaginationDetails(pageData),
	})
}

func (s *server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	if s.limiter != nil && !s.limiter.allow(clientIP(r)) {
		rejectSubmission(w, r, http.StatusTooManyRequests, "too many submissions, please try again later")
		return
	}

	contentType := r.Header.Get("Content-Type")

	var entry Entry
	var honeypot string

	if strings.HasPrefix(contentType, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodySize)
		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			rejectSubmission(w, r, http.StatusBadRequest, "request too large")
			return
		}
		entry.Name = strings.TrimSpace(r.FormValue("name"))
		entry.Website = strings.TrimSpace(r.FormValue("website"))
		entry.EntryType = normalizeEntryType(r.FormValue("entry_type"))
		entry.Content = normalizeContent(r.FormValue("content"))
		honeypot = r.FormValue("gb_verify")

		file, _, err := r.FormFile("image")
		if err == nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxImageSize+1))
			if err != nil {
				rejectSubmission(w, r, http.StatusBadRequest, "error reading image")
				return
			}
			if len(data) > maxImageSize {
				rejectSubmission(w, r, http.StatusBadRequest, "image must be 5MB or smaller")
				return
			}
			if !isPNG(data) {
				rejectSubmission(w, r, http.StatusBadRequest, "image must be a PNG file")
				return
			}
			cfg, pngErr := png.DecodeConfig(bytes.NewReader(data))
			if pngErr != nil {
				rejectSubmission(w, r, http.StatusBadRequest, "image must be a valid PNG file")
				return
			}
			if cfg.Width*cfg.Height > 2_000_000 {
				rejectSubmission(w, r, http.StatusBadRequest, "image dimensions too large")
				return
			}
			entry.ImageData = data
		} else if !errors.Is(err, http.ErrMissingFile) {
			rejectSubmission(w, r, http.StatusBadRequest, "error reading image")
			return
		}
	} else {
		var req struct {
			Name      string `json:"name"`
			Website   string `json:"website"`
			EntryType string `json:"entry_type"`
			Content   string `json:"content"`
			Verify    string `json:"gb_verify"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			rejectSubmission(w, r, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry.Name = strings.TrimSpace(req.Name)
		entry.Website = strings.TrimSpace(req.Website)
		entry.EntryType = normalizeEntryType(req.EntryType)
		entry.Content = normalizeContent(req.Content)
		honeypot = req.Verify
	}

	// The sign form hides the gb_verify field from people, so anything in it
	// came from a bot. Reply exactly like a real submission so the bot has no
	// signal to adapt to, but store nothing and send no notification.
	if strings.TrimSpace(honeypot) != "" {
		log.Printf("submission dropped: honeypot filled (ua=%q)", truncate(r.UserAgent(), 120))
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":      0,
			"message": "entry submitted for review",
		})
		return
	}

	website, err := normalizeWebsite(entry.Website)
	if err != nil {
		rejectSubmission(w, r, http.StatusBadRequest, err.Error())
		return
	}
	entry.Website = website

	if err := validateEntry(entry); err != nil {
		rejectSubmission(w, r, http.StatusBadRequest, err.Error())
		return
	}

	id, err := createEntry(s.db, &entry)
	if err != nil {
		log.Printf("error creating entry: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log.Printf("submission accepted: id=%d type=%s", id, entry.EntryType)

	if s.telegram != nil {
		entry.ID = id
		s.telegram.notifyNewEntry(entry)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      id,
		"message": "entry submitted for review",
	})
}

func (s *server) handleGetImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}

	data, err := getEntryImage(s.db, id, "approved")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		log.Printf("error getting image: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *server) handleAdminGetImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}

	data, err := getEntryImage(s.db, id, "")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		log.Printf("error getting admin image: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(data)
}

func (s *server) handleAdminGetEntries(w http.ResponseWriter, r *http.Request) {
	entries, err := getPendingEntries(s.db)
	if err != nil {
		log.Printf("error getting pending entries: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	baseURL := requestBaseURL(r)
	responses := make([]entryResponse, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, buildEntryResponse(entry, baseURL, fmt.Sprintf("/api/admin/entries/%d/image.png", entry.ID), true))
	}

	writeJSON(w, http.StatusOK, pendingEntryListResponse{Entries: responses})
}

func (s *server) handleApproveEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := approveEntry(s.db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "entry not found or not pending")
			return
		}
		log.Printf("error approving entry: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log.Printf("admin: approved entry %d from %s", id, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"message": "entry approved"})
}

func (s *server) handleRejectEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := rejectEntry(s.db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "entry not found or not pending")
			return
		}
		log.Printf("error rejecting entry: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log.Printf("admin: rejected entry %d from %s", id, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"message": "entry rejected"})
}

func (s *server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := deleteEntry(s.db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "entry not found")
			return
		}
		log.Printf("error deleting entry: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log.Printf("admin: deleted entry %d from %s", id, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"message": "entry deleted"})
}

func (s *server) handlePurgeRejected(w http.ResponseWriter, r *http.Request) {
	count, err := purgeRejectedEntries(s.db)
	if err != nil {
		log.Printf("error purging rejected entries: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	log.Printf("admin: purged %d rejected entries from %s", count, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "rejected entries purged",
		"deleted": count,
	})
}

func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.adminToken == "" {
			writeJSONError(w, http.StatusInternalServerError, "admin not configured")
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		next(w, r)
	}
}

func buildEntryResponse(entry Entry, baseURL, imagePath string, includeStatus bool) entryResponse {
	response := entryResponse{
		ID:        entry.ID,
		Name:      entry.Name,
		Website:   entry.Website,
		EntryType: entry.EntryType,
		Content:   entry.Content,
		HasImage:  entry.HasImage,
		CreatedAt: entry.CreatedAt,
	}
	if entry.HasImage {
		response.ImageURL = baseURL + imagePath
	}
	if includeStatus {
		response.Status = entry.Status
	}
	return response
}

func buildPaginationDetails(pageData EntryPage) paginationDetails {
	totalPages := pageData.TotalPages()
	pagination := paginationDetails{
		Page:         pageData.Page,
		PerPage:      pageData.PerPage,
		TotalEntries: pageData.TotalEntries,
		TotalPages:   totalPages,
		HasPrevious:  pageData.Page > 1,
		HasNext:      pageData.Page < totalPages,
	}
	if pagination.HasPrevious {
		pagination.PreviousPage = pageData.Page - 1
	}
	if pagination.HasNext {
		pagination.NextPage = pageData.Page + 1
	}
	return pagination
}

func normalizeEntryType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "draw", "drawing":
		return "drawing"
	case "message", "text":
		return "message"
	default:
		return ""
	}
}

func normalizeContent(raw string) string {
	return strings.ReplaceAll(raw, "\r\n", "\n")
}

func normalizeWebsite(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > maxWebsiteLength {
		return "", fmt.Errorf("website must be %d characters or fewer", maxWebsiteLength)
	}
	errInvalid := errors.New("website must be a valid http or https URL")
	if hasUnsafeURLChar(raw) {
		return "", errInvalid
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", errInvalid
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errInvalid
	}
	// url.URL.String keeps RawQuery verbatim, so re-check the output too.
	normalized := parsed.String()
	if hasUnsafeURLChar(normalized) {
		return "", errInvalid
	}
	return normalized, nil
}

// hasUnsafeURLChar reports characters that could break out of an HTML
// attribute or are never valid unescaped in a URL.
func hasUnsafeURLChar(s string) bool {
	for _, r := range s {
		if r <= ' ' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
		switch r {
		case '"', '\'', '<', '>', '`':
			return true
		}
	}
	return false
}

func validateEntry(entry Entry) error {
	if entry.Name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(entry.Name) > maxNameRunes {
		return fmt.Errorf("name must be %d characters or fewer", maxNameRunes)
	}
	if entry.EntryType != "drawing" && entry.EntryType != "message" {
		return errors.New("entry_type must be 'drawing' or 'message'")
	}
	if utf8.RuneCountInString(entry.Content) > maxContentRunes {
		return fmt.Errorf("message must be %d characters or fewer", maxContentRunes)
	}
	if entry.EntryType == "message" && strings.TrimSpace(entry.Content) == "" {
		return errors.New("content is required for message entries")
	}
	if entry.EntryType == "drawing" && len(entry.ImageData) == 0 {
		return errors.New("image is required for drawing entries")
	}
	if entry.EntryType == "message" && len(entry.ImageData) > 0 {
		return errors.New("image is only allowed for drawing entries")
	}
	return nil
}

func parsePositiveInt(raw string, defaultValue, minValue, maxValue int) int {
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if forwardedProto := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); forwardedProto == "https" {
		scheme = "https"
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("error marshaling json response: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
	w.Write([]byte("\n"))
}

// rejectSubmission logs why a guestbook submission was turned away before
// replying, so "nobody submitted" can be told apart from "submits are failing".
// It logs the reason and user agent, not the name, content or IP.
func rejectSubmission(w http.ResponseWriter, r *http.Request, status int, message string) {
	log.Printf("submission rejected: status=%d reason=%q ua=%q", status, message, truncate(r.UserAgent(), 120))
	writeJSONError(w, status, message)
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func isPNG(data []byte) bool {
	return len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature)
}
