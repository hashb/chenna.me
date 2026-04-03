package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.hacdias.com/indielib/micropub"
)

// categories available for micro-posts, matching _config.yml prose tags.
var defaultCategories = []string{
	"micro", "photos", "links", "tech", "random", "til",
}

type jekyllMicropub struct {
	repo          *gitRepo
	gcs           *gcsUploader
	imageBaseURL  string
	siteURL       string
	tokenEndpoint string
}

// HasScope verifies the bearer token against the IndieAuth token endpoint.
func (j *jekyllMicropub) HasScope(r *http.Request, scope string) bool {
	token := extractBearerToken(r)
	if token == "" {
		return false
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, j.tokenEndpoint, nil)
	if err != nil {
		log.Printf("error creating token verification request: %v", err)
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("error verifying token: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("token verification failed: status %d", resp.StatusCode)
		return false
	}

	var tokenResp struct {
		Me       string `json:"me"`
		Scope    string `json:"scope"`
		ClientID string `json:"client_id"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("error reading token response: %v", err)
		return false
	}

	// Try JSON first, fall back to form-encoded
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		// Some token endpoints return form-encoded
		for _, pair := range strings.Split(string(body), "&") {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "me":
				tokenResp.Me = kv[1]
			case "scope":
				tokenResp.Scope = kv[1]
			}
		}
	}

	// Verify the "me" URL matches our site
	if !strings.HasPrefix(tokenResp.Me, j.siteURL) {
		log.Printf("token 'me' mismatch: got %q, want prefix %q", tokenResp.Me, j.siteURL)
		return false
	}

	// Check scope
	for _, s := range strings.Fields(tokenResp.Scope) {
		if s == scope {
			return true
		}
	}

	// "create" scope implies "media" scope
	if scope == "media" {
		for _, s := range strings.Fields(tokenResp.Scope) {
			if s == "create" {
				return true
			}
		}
	}

	log.Printf("scope %q not found in %q", scope, tokenResp.Scope)
	return false
}

// hasScope is the ScopeChecker for the media handler.
func (j *jekyllMicropub) hasScope(r *http.Request, scope string) bool {
	return j.HasScope(r, scope)
}

// Create creates a new micro-post.
func (j *jekyllMicropub) Create(req *micropub.Request) (string, error) {
	now := time.Now().UTC()

	content := extractContent(req.Properties)
	photos := extractStringSlice(req.Properties, "photo")
	categories := extractStringSlice(req.Properties, "category")
	photoAlts := extractStringSlice(req.Commands, "photo-alt")

	// Determine published status
	published := true
	if statuses := extractStringSlice(req.Commands, "post-status"); len(statuses) > 0 {
		if statuses[0] == "draft" {
			published = false
		}
	}

	// Use provided date or now
	postDate := now
	if dates := extractStringSlice(req.Properties, "published"); len(dates) > 0 {
		if t, err := time.Parse(time.RFC3339, dates[0]); err == nil {
			postDate = t.UTC()
		}
	}

	// Build the post content
	var body strings.Builder

	if content != "" {
		body.WriteString(content)
		body.WriteString("\n")
	}

	// Append photos that aren't already inline in HTML content
	for i, photo := range photos {
		alt := ""
		if i < len(photoAlts) {
			alt = photoAlts[i]
		}

		// If the photo URL matches our CDN, use responsive_image include
		if strings.Contains(photo, j.imageBaseURL) {
			baseName := extractBaseName(photo, j.imageBaseURL)
			body.WriteString(fmt.Sprintf("\n{%% include responsive_image.html base_image_name=%q alt=%q width=\"1920\" height=\"auto\" %%}\n", baseName, alt))
		} else {
			body.WriteString(fmt.Sprintf("\n<img src=%q alt=%q style=\"max-width: 100%%; height: auto;\">\n", photo, alt))
		}
	}

	// Add default "micro" tag if no categories specified
	if len(categories) == 0 {
		categories = []string{"micro"}
	}

	// Build front matter
	// URL is derived entirely from the date via permalink: /micro/:year/:month/:day/:hour:minute:second/
	filename := fmt.Sprintf("_micros/%d/%s.md", postDate.Year(), postDate.Format("2006-01-02-150405"))
	postURL := fmt.Sprintf("%s/micro/%s/", j.siteURL, postDate.Format("2006/01/02/150405"))

	frontMatter := buildFrontMatter(postDate, categories, published)
	fullContent := frontMatter + "\n" + body.String()

	commitMsg := fmt.Sprintf("micropub: create micro-post %s", postDate.Format("2006-01-02T150405"))
	if err := j.repo.writeAndPush(filename, fullContent, commitMsg); err != nil {
		return "", fmt.Errorf("failed to write post: %w", err)
	}

	log.Printf("created micro-post: %s", filename)
	return postURL, nil
}

// Update modifies an existing micro-post.
func (j *jekyllMicropub) Update(req *micropub.Request) (string, error) {
	filename, err := j.urlToFilename(req.URL)
	if err != nil {
		return "", err
	}

	data, err := j.repo.readFile(filename)
	if err != nil {
		return "", micropub.ErrNotFound
	}

	fm, content := parseFrontMatter(string(data))

	// Apply replacements
	if req.Updates.Replace != nil {
		if vals, ok := req.Updates.Replace["content"]; ok && len(vals) > 0 {
			content = fmt.Sprintf("%v", vals[0])
		}
		if vals, ok := req.Updates.Replace["category"]; ok {
			fm["tags"] = toStringSlice(vals)
		}
		if vals, ok := req.Updates.Replace["post-status"]; ok && len(vals) > 0 {
			fm["published"] = fmt.Sprintf("%v", vals[0]) != "draft"
		}
	}

	// Apply additions
	if req.Updates.Add != nil {
		if vals, ok := req.Updates.Add["category"]; ok {
			existing, _ := fm["tags"].([]string)
			fm["tags"] = append(existing, toStringSlice(vals)...)
		}
	}

	// Apply deletions
	if deleteMap, ok := req.Updates.Delete.(map[string]any); ok {
		if vals, ok := deleteMap["category"]; ok {
			existing, _ := fm["tags"].([]string)
			toRemove := toStringSlice(vals.([]any))
			fm["tags"] = removeStrings(existing, toRemove)
		}
	} else if deleteSlice, ok := req.Updates.Delete.([]any); ok {
		for _, key := range deleteSlice {
			if k, ok := key.(string); ok {
				if k == "category" {
					fm["tags"] = []string{}
				}
			}
		}
	}

	fullContent := rebuildPost(fm, content)
	commitMsg := fmt.Sprintf("micropub: update micro-post %s", filepath.Base(filename))
	if err := j.repo.updateAndPush(filename, fullContent, commitMsg); err != nil {
		return "", fmt.Errorf("failed to update post: %w", err)
	}

	log.Printf("updated micro-post: %s", filename)
	return req.URL, nil
}

// Delete removes a micro-post.
func (j *jekyllMicropub) Delete(url string) error {
	filename, err := j.urlToFilename(url)
	if err != nil {
		return err
	}

	commitMsg := fmt.Sprintf("micropub: delete micro-post %s", filepath.Base(filename))
	if err := j.repo.deleteAndPush(filename, commitMsg); err != nil {
		return fmt.Errorf("failed to delete post: %w", err)
	}

	log.Printf("deleted micro-post: %s", filename)
	return nil
}

// Undelete is not supported.
func (j *jekyllMicropub) Undelete(url string) error {
	return micropub.ErrNotImplemented
}

// Source returns the microformats source of a post.
func (j *jekyllMicropub) Source(url string) (map[string]any, error) {
	filename, err := j.urlToFilename(url)
	if err != nil {
		return nil, err
	}

	data, err := j.repo.readFile(filename)
	if err != nil {
		return nil, micropub.ErrNotFound
	}

	return postToMf2(string(data), url), nil
}

// SourceMany returns a list of micro-posts.
func (j *jekyllMicropub) SourceMany(limit, offset int) ([]map[string]any, error) {
	files, err := j.repo.listFiles("_micros/**/*.md")
	if err != nil {
		return nil, fmt.Errorf("listing posts: %w", err)
	}

	// Sort reverse chronologically (filenames are date-based)
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	// Apply offset
	if offset > 0 && offset < len(files) {
		files = files[offset:]
	} else if offset >= len(files) {
		return []map[string]any{}, nil
	}

	// Apply limit
	if limit > 0 && limit < len(files) {
		files = files[:limit]
	}

	items := make([]map[string]any, 0, len(files))
	for _, f := range files {
		rel, _ := filepath.Rel(j.repo.path, f)
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		url := j.filenameToURL(rel)
		items = append(items, postToMf2(string(data), url))
	}

	return items, nil
}

// getCategories returns available categories.
func (j *jekyllMicropub) getCategories() []string {
	return defaultCategories
}

// --- helpers ---

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Also check form value per spec
	if token := r.FormValue("access_token"); token != "" {
		return token
	}
	return ""
}

func extractContent(props map[string][]any) string {
	if vals, ok := props["content"]; ok && len(vals) > 0 {
		switch v := vals[0].(type) {
		case string:
			return v
		case map[string]any:
			// Sunlit sends {"html": "<p>...</p>"}
			if html, ok := v["html"]; ok {
				if s, ok := html.(string); ok {
					return s
				}
			}
			if text, ok := v["value"]; ok {
				if s, ok := text.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

func extractStringSlice(props map[string][]any, key string) []string {
	vals, ok := props[key]
	if !ok {
		return nil
	}
	return toStringSlice(vals)
}

func toStringSlice(vals []any) []string {
	var result []string
	for _, v := range vals {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func extractBaseName(photoURL, baseURL string) string {
	// Remove the base URL prefix to get the image name
	name := strings.TrimPrefix(photoURL, "https:"+baseURL+"/")
	name = strings.TrimPrefix(name, "http:"+baseURL+"/")
	name = strings.TrimPrefix(name, baseURL+"/")
	// Remove size suffix if present
	for _, suffix := range []string{"-small", "-medium", "-large", "-xlarge"} {
		name = strings.TrimSuffix(name, suffix+".jpg")
		name = strings.TrimSuffix(name, suffix+".jpeg")
		name = strings.TrimSuffix(name, suffix+".png")
	}
	// Include the micro/ prefix for responsive_image include
	return "micro/" + name
}

func buildFrontMatter(date time.Time, tags []string, published bool) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("layout: micro\n")
	b.WriteString(fmt.Sprintf("date: %s\n", date.Format("2006-01-02 15:04:05 -0700")))
	b.WriteString("tags:\n")
	for _, tag := range tags {
		b.WriteString(fmt.Sprintf("  - %s\n", tag))
	}
	if published {
		b.WriteString("published: true\n")
	} else {
		b.WriteString("published: false\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// parseFrontMatter splits a Jekyll post into front matter map and content body.
func parseFrontMatter(data string) (map[string]any, string) {
	fm := map[string]any{}

	if !strings.HasPrefix(data, "---\n") {
		return fm, data
	}

	end := strings.Index(data[4:], "\n---\n")
	if end == -1 {
		return fm, data
	}

	fmText := data[4 : end+4]
	content := strings.TrimPrefix(data[end+8:], "\n")

	// Simple YAML parsing for our known fields
	for _, line := range strings.Split(fmText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "layout":
			fm["layout"] = val
		case "date":
			fm["date"] = val
		case "published":
			fm["published"] = val == "true"
		case "tags":
			// Tags are on subsequent lines starting with "  - "
			var tags []string
			for _, tl := range strings.Split(fmText, "\n") {
				tl = strings.TrimSpace(tl)
				if strings.HasPrefix(tl, "- ") {
					tags = append(tags, strings.TrimPrefix(tl, "- "))
				}
			}
			fm["tags"] = tags
		}
	}

	return fm, content
}

// rebuildPost reconstructs a Jekyll post from front matter and content.
func rebuildPost(fm map[string]any, content string) string {
	date, _ := fm["date"].(string)
	tags, _ := fm["tags"].([]string)
	published, _ := fm["published"].(bool)

	layout, ok := fm["layout"].(string)
	if !ok {
		layout = "micro"
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("layout: %s\n", layout))
	if date != "" {
		b.WriteString(fmt.Sprintf("date: %s\n", date))
	}
	b.WriteString("tags:\n")
	for _, tag := range tags {
		b.WriteString(fmt.Sprintf("  - %s\n", tag))
	}
	if published {
		b.WriteString("published: true\n")
	} else {
		b.WriteString("published: false\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(content)
	return b.String()
}

// urlToFilename converts a post URL to a file path in the repo.
// URL format: https://chenna.me/micro/2026/04/03/143000/
// File format: _micros/2026/2026-04-03-143000.md
func (j *jekyllMicropub) urlToFilename(postURL string) (string, error) {
	path := strings.TrimPrefix(postURL, j.siteURL)
	path = strings.TrimPrefix(path, "/micro/")
	path = strings.TrimSuffix(path, "/")

	// Expected: "2026/04/03/143000"
	parts := strings.Split(path, "/")
	if len(parts) != 4 {
		return "", fmt.Errorf("%w: invalid post URL format: %s", micropub.ErrBadRequest, postURL)
	}

	year, month, day, time := parts[0], parts[1], parts[2], parts[3]
	filename := fmt.Sprintf("_micros/%s/%s-%s-%s-%s.md", year, year, month, day, time)
	return filename, nil
}

// filenameToURL converts a file path to a post URL.
// File format: _micros/2026/2026-04-03-143000.md
// URL format: https://chenna.me/micro/2026/04/03/143000/
func (j *jekyllMicropub) filenameToURL(filename string) string {
	name := strings.TrimPrefix(filename, "_micros/")
	name = strings.TrimSuffix(name, ".md")

	// Expected: "2026/2026-04-03-143000"
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return j.siteURL + "/micro/"
	}

	// Parse "2026-04-03-143000" → year, month, day, time
	dateParts := strings.SplitN(parts[1], "-", 4)
	if len(dateParts) != 4 {
		return j.siteURL + "/micro/"
	}

	return fmt.Sprintf("%s/micro/%s/%s/%s/%s/", j.siteURL, dateParts[0], dateParts[1], dateParts[2], dateParts[3])
}

// postToMf2 converts a Jekyll post file to microformats2 properties.
func postToMf2(data, url string) map[string]any {
	fm, content := parseFrontMatter(data)

	props := map[string]any{
		"url": []any{url},
	}

	if content != "" {
		props["content"] = []any{content}
	}

	if date, ok := fm["date"].(string); ok {
		props["published"] = []any{date}
	}

	if tags, ok := fm["tags"].([]string); ok && len(tags) > 0 {
		cats := make([]any, len(tags))
		for i, t := range tags {
			cats[i] = t
		}
		props["category"] = cats
	}

	if published, ok := fm["published"].(bool); ok && !published {
		props["post-status"] = []any{"draft"}
	}

	return map[string]any{
		"type":       []string{"h-entry"},
		"properties": props,
	}
}

func removeStrings(from, remove []string) []string {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[r] = true
	}
	var result []string
	for _, s := range from {
		if !removeSet[s] {
			result = append(result, s)
		}
	}
	return result
}
