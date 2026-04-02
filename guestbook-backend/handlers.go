package main

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultPublicPerPage = 24
	maxPublicPerPage     = 48
	maxNameRunes         = 80
	maxWebsiteLength     = 2048
	maxContentRunes      = 4000
	maxJSONBodySize      = 64 << 10 // 64KB
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
	mux        *http.ServeMux
}

func newServer(db *sql.DB, adminToken string, limiter *rateLimiter) *server {
	s := &server{
		db:         db,
		adminToken: adminToken,
		limiter:    limiter,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /api/entries", s.handleGetEntries)
	s.mux.HandleFunc("POST /api/entries", s.handleCreateEntry)
	s.mux.HandleFunc("GET /api/entries/{id}/image", s.handleGetImage)
	s.mux.HandleFunc("GET /api/admin/entries/{id}/image", s.requireAdmin(s.handleAdminGetImage))
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
		responses = append(responses, buildEntryResponse(entry, baseURL, fmt.Sprintf("/api/entries/%d/image", entry.ID), false))
	}

	writeJSON(w, http.StatusOK, entryListResponse{
		Entries:    responses,
		Pagination: buildPaginationDetails(pageData),
	})
}

func (s *server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	if s.limiter != nil && !s.limiter.allow(clientIP(r)) {
		writeJSONError(w, http.StatusTooManyRequests, "too many submissions, please try again later")
		return
	}

	contentType := r.Header.Get("Content-Type")

	var entry Entry

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
			writeJSONError(w, http.StatusBadRequest, "request too large")
			return
		}
		entry.Name = strings.TrimSpace(r.FormValue("name"))
		entry.Website = strings.TrimSpace(r.FormValue("website"))
		entry.EntryType = normalizeEntryType(r.FormValue("entry_type"))
		entry.Content = normalizeContent(r.FormValue("content"))

		file, _, err := r.FormFile("image")
		if err == nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "error reading image")
				return
			}
			if len(data) > 5<<20 {
				writeJSONError(w, http.StatusBadRequest, "image must be 5MB or smaller")
				return
			}
			if !isPNG(data) {
				writeJSONError(w, http.StatusBadRequest, "image must be a PNG file")
				return
			}
			entry.ImageData = data
		} else if !errors.Is(err, http.ErrMissingFile) {
			writeJSONError(w, http.StatusBadRequest, "error reading image")
			return
		}
	} else {
		var req struct {
			Name      string `json:"name"`
			Website   string `json:"website"`
			EntryType string `json:"entry_type"`
			Content   string `json:"content"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry.Name = strings.TrimSpace(req.Name)
		entry.Website = strings.TrimSpace(req.Website)
		entry.EntryType = normalizeEntryType(req.EntryType)
		entry.Content = normalizeContent(req.Content)
	}

	website, err := normalizeWebsite(entry.Website)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry.Website = website

	if err := validateEntry(entry); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := createEntry(s.db, &entry)
	if err != nil {
		log.Printf("error creating entry: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      id,
		"message": "entry submitted for review",
	})
}

func (s *server) handleGetImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	data, err := getEntryImage(s.db, id, "approved")
	if errors.Is(err, sql.ErrNoRows) || data == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("error getting image: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *server) handleAdminGetImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	data, err := getEntryImage(s.db, id, "")
	if errors.Is(err, sql.ErrNoRows) || data == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("error getting admin image: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=0")
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
		responses = append(responses, buildEntryResponse(entry, baseURL, fmt.Sprintf("/api/admin/entries/%d/image", entry.ID), true))
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

	writeJSON(w, http.StatusOK, map[string]string{"message": "entry deleted"})
}

func (s *server) handlePurgeRejected(w http.ResponseWriter, r *http.Request) {
	count, err := purgeRejectedEntries(s.db)
	if err != nil {
		log.Printf("error purging rejected entries: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

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
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return "", errors.New("website must be a valid http or https URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("website must be a valid http or https URL")
	}
	return parsed.String(), nil
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
	if forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwardedProto != "" {
		scheme = forwardedProto
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("error writing json response: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func isPNG(data []byte) bool {
	return len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature)
}
