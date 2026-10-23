package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.hacdias.com/indielib/indieauth"
	"go.hacdias.com/indielib/micropub"
)

// categories available for micro-posts, matching _config.yml prose tags.
var defaultCategories = []string{
	"micro", "photos", "links", "tech", "random", "til",
}

type jekyllMicropub struct {
	repo                  *gitRepo
	gcs                   *gcsUploader
	imageBaseURL          string
	honorCreatePostStatus bool
	siteURL               string
	endpointURL           string
	tokenEndpoint         string
	// thumbhashCache and exifDateCache are filled by uploadMedia and read by
	// Create, which deletes the entries for its photos once the post is pushed. Values are either the raw value (string /
	// time.Time) or a mediaCacheEntry wrapping it with the time it was stored;
	// only wrapped entries can be pruned by age (see rememberMediaMetadata).
	thumbhashCache sync.Map // cdnURL -> base64 ThumbHash string
	exifDateCache  sync.Map // cdnURL -> time.Time (EXIF DateTimeOriginal)
}

// mediaCacheTTL bounds how long upload metadata is kept waiting for a Create
// that references it.
const mediaCacheTTL = 24 * time.Hour

// maxCreateCollisionRetries bounds how many times Create bumps the post date
// by one second to find a free filename/URL.
const maxCreateCollisionRetries = 60

type mediaCacheEntry struct {
	value    any
	storedAt time.Time
}

// rememberMediaMetadata stores upload metadata for a later Create, with a
// timestamp so stale entries (uploads never followed by a Create) can be
// pruned. thumbhash may be empty and exifDate may be nil.
func (j *jekyllMicropub) rememberMediaMetadata(cdnURL, thumbhash string, exifDate *time.Time) {
	now := time.Now()
	if thumbhash != "" {
		j.thumbhashCache.Store(cdnURL, mediaCacheEntry{value: thumbhash, storedAt: now})
	}
	if exifDate != nil {
		j.exifDateCache.Store(cdnURL, mediaCacheEntry{value: *exifDate, storedAt: now})
	}
}

// loadMediaCacheValue returns the cached value for key, accepting both raw
// and timestamped (mediaCacheEntry) values.
func loadMediaCacheValue(m *sync.Map, key string) (any, bool) {
	v, ok := m.Load(key)
	if !ok {
		return nil, false
	}
	if entry, ok := v.(mediaCacheEntry); ok {
		return entry.value, true
	}
	return v, true
}

// forgetMediaMetadata drops the cached upload metadata for photos that a
// post has now consumed. It runs only after a successful create, so a failed
// create can be retried by the client without losing the EXIF date/ThumbHash.
// A later post reusing the same photo URL will not get them.
func (j *jekyllMicropub) forgetMediaMetadata(photos []photoReference) {
	for _, photo := range photos {
		j.thumbhashCache.Delete(photo.URL)
		j.exifDateCache.Delete(photo.URL)
	}
}

// pruneMediaCaches drops timestamped cache entries older than mediaCacheTTL.
func (j *jekyllMicropub) pruneMediaCaches(now time.Time) {
	for _, m := range []*sync.Map{&j.thumbhashCache, &j.exifDateCache} {
		m.Range(func(key, value any) bool {
			if entry, ok := value.(mediaCacheEntry); ok && now.Sub(entry.storedAt) > mediaCacheTTL {
				m.CompareAndDelete(key, value)
			}
			return true
		})
	}
}

type tokenVerificationResponse struct {
	Me       string `json:"me"`
	Scope    string `json:"scope"`
	ClientID string `json:"client_id"`
}

var tokenHTTPClient = &http.Client{Timeout: 10 * time.Second}

const maxTokenResponseSize = 1 << 20 // 1 MB

// HasScope verifies the bearer token against the IndieAuth token endpoint.
func (j *jekyllMicropub) HasScope(r *http.Request, scope string) bool {
	tokenResp, err := j.verifyToken(r)
	if err != nil {
		log.Printf("token verification failed: %v", err)
		return false
	}

	if tokenResponseHasScope(tokenResp, scope) {
		return true
	}

	log.Printf("scope %q not found in %q", scope, tokenResp.Scope)
	return false
}

func (j *jekyllMicropub) verifyToken(r *http.Request) (tokenVerificationResponse, error) {
	token := extractBearerToken(r)
	if token == "" {
		return tokenVerificationResponse{}, fmt.Errorf("missing bearer token")
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, j.tokenEndpoint, nil)
	if err != nil {
		return tokenVerificationResponse{}, fmt.Errorf("create token verification request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := tokenHTTPClient.Do(req)
	if err != nil {
		return tokenVerificationResponse{}, fmt.Errorf("verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tokenVerificationResponse{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseSize))
	if err != nil {
		return tokenVerificationResponse{}, fmt.Errorf("read token response: %w", err)
	}

	tokenResp, err := parseTokenVerificationResponse(body)
	if err != nil {
		return tokenVerificationResponse{}, fmt.Errorf("parse token response: %w", err)
	}

	if err := verifyProfileURLMatch(tokenResp.Me, j.siteURL); err != nil {
		return tokenVerificationResponse{}, err
	}

	return tokenResp, nil
}

func tokenResponseHasScope(tokenResp tokenVerificationResponse, scope string) bool {
	for _, s := range strings.Fields(tokenResp.Scope) {
		if s == scope {
			return true
		}
	}

	if scope == "media" {
		for _, s := range strings.Fields(tokenResp.Scope) {
			if s == "create" {
				return true
			}
		}
	}

	return false
}

// hasScope is the ScopeChecker for the media handler.
func (j *jekyllMicropub) hasScope(r *http.Request, scope string) bool {
	return j.HasScope(r, scope)
}

// Create creates a new micro-post.
func (j *jekyllMicropub) Create(req *micropub.Request) (string, error) {
	now := time.Now().UTC()
	j.pruneMediaCaches(now)

	content := extractContent(req.Properties)
	photos := extractPhotos(req.Properties, req.Commands)
	categories := extractStringSlice(req.Properties, "category")

	// Determine published status.
	published := requestedPublishedStatus(req, j.honorCreatePostStatus)

	// Use provided date, EXIF date from the first photo, or now (in that priority order).
	postDate := now
	if dates := extractStringSlice(req.Properties, "published"); len(dates) > 0 {
		if t, err := time.Parse(time.RFC3339, dates[0]); err == nil {
			postDate = t.UTC()
		}
	} else if len(photos) > 0 {
		if v, ok := loadMediaCacheValue(&j.exifDateCache, photos[0].URL); ok {
			if t, ok := v.(time.Time); ok {
				postDate = t.UTC()
			}
		}
	}

	// Build the post content
	var body strings.Builder

	if content != "" {
		body.WriteString(content)
		body.WriteString("\n")
	}

	// Append photos that aren't already inline in HTML content
	for _, photo := range photos {
		// If the photo URL matches our CDN, use responsive_image include
		th := ""
		if v, ok := loadMediaCacheValue(&j.thumbhashCache, photo.URL); ok {
			th, _ = v.(string)
		}
		baseName := ""
		if isManagedPhotoURL(photo.URL, j.imageBaseURL) {
			baseName = extractBaseName(photo.URL, j.imageBaseURL)
		}
		if baseName != "" && isSafeIncludeBaseName(baseName) {
			if !isSafeThumbhash(th) {
				th = ""
			}
			body.WriteString(fmt.Sprintf("\n{%% include responsive_image.html base_image_name=%s alt=%s width=\"1920\" height=\"auto\" thumbhash=%s %%}\n",
				jekyllIncludeValue(baseName), jekyllIncludeValue(photo.Alt), jekyllIncludeValue(th)))
		} else {
			body.WriteString(fmt.Sprintf("\n<img src=\"%s\" alt=\"%s\" style=\"max-width: 100%%; height: auto;\">\n",
				html.EscapeString(neutralizeLiquidURL(photo.URL)), html.EscapeString(neutralizeLiquid(strings.Join(strings.Fields(photo.Alt), " ")))))
		}
	}

	// Auto-add "photos" tag when the post contains images
	if len(photos) > 0 && !containsStringFold(categories, "photos") {
		categories = append(categories, "photos")
	}

	// Add default "micro" tag if no categories specified
	if len(categories) == 0 {
		categories = []string{"micro"}
	}

	// The URL is derived entirely from the front matter date via the _micros
	// permalink /notes/:year/:month/:day/:hour:minute:second/ (site timezone
	// is UTC), and the filename uses the same second-resolution timestamp. A
	// post must never replace an existing one, so on collision the date is
	// bumped by one second and the create is retried, keeping filename, front
	// matter date and URL consistent.
	for attempt := 0; attempt < maxCreateCollisionRetries; attempt++ {
		candidate := postDate.Add(time.Duration(attempt) * time.Second)
		filename := postFilename(candidate)

		// Cheap pre-check against the local checkout; createAndPush re-checks
		// atomically (O_EXCL) after syncing with the remote.
		if _, err := os.Stat(filepath.Join(j.repo.path, filename)); err == nil {
			continue
		}

		frontMatter, err := buildFrontMatter(candidate, categories, published)
		if err != nil {
			return "", fmt.Errorf("build front matter: %w", err)
		}
		fullContent := frontMatter + "\n" + body.String()

		commitMsg := fmt.Sprintf("micropub: create micro-post %s", candidate.Format("2006-01-02T150405"))
		if err := j.repo.createAndPush(filename, fullContent, commitMsg); err != nil {
			if errors.Is(err, errFileExists) {
				continue
			}
			return "", fmt.Errorf("failed to write post: %w", err)
		}

		j.forgetMediaMetadata(photos)
		if attempt > 0 {
			log.Printf("post date %s was taken; used %s instead", postDate.Format(time.RFC3339), candidate.Format(time.RFC3339))
		}
		log.Printf("created micro-post: %s", filename)
		return fmt.Sprintf("%s/notes/%s/", j.siteURL, candidate.Format("2006/01/02/150405")), nil
	}

	return "", fmt.Errorf("failed to write post: no free post slot within %d seconds of %s", maxCreateCollisionRetries, postDate.Format(time.RFC3339))
}

func postFilename(date time.Time) string {
	return fmt.Sprintf("_micros/%d/%s.md", date.Year(), date.Format("2006-01-02-150405"))
}

// Update modifies an existing micro-post.
func (j *jekyllMicropub) Update(req *micropub.Request) (string, error) {
	filename, err := j.urlToFilename(req.URL)
	if err != nil {
		return "", err
	}

	commitMsg := fmt.Sprintf("micropub: update micro-post %s", filepath.Base(filename))
	err = j.repo.editAndPush(filename, commitMsg, func(data []byte) (string, error) {
		return applyPostUpdate(string(data), req.Updates)
	})
	if errors.Is(err, os.ErrNotExist) {
		return "", micropub.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to update post: %w", err)
	}

	log.Printf("updated micro-post: %s", filename)
	return req.URL, nil
}

func applyPostUpdate(data string, updates micropub.RequestUpdate) (string, error) {
	fm, content, err := parseFrontMatter(data)
	if err != nil {
		return "", fmt.Errorf("parse post: %w", err)
	}

	// Apply replacements
	if updates.Replace != nil {
		if vals, ok := updates.Replace["content"]; ok && len(vals) > 0 {
			newContent, ok := contentValueToString(vals[0])
			if !ok {
				return "", fmt.Errorf("%w: unsupported content value type %T", micropub.ErrBadRequest, vals[0])
			}
			content = newContent
		}
		if vals, ok := updates.Replace["category"]; ok {
			fm.Tags = toStringSlice(vals)
		}
		if vals, ok := updates.Replace["post-status"]; ok && len(vals) > 0 {
			status, ok := vals[0].(string)
			if !ok {
				return "", fmt.Errorf("%w: post-status must be a string", micropub.ErrBadRequest)
			}
			published := !strings.EqualFold(status, "draft")
			fm.Published = publishedFrontMatterValue(published)
		}
	}

	// Apply additions
	if updates.Add != nil {
		if vals, ok := updates.Add["category"]; ok {
			fm.Tags = append(fm.Tags, toStringSlice(vals)...)
		}
	}

	// Apply deletions
	if deleteMap, ok := updates.Delete.(map[string]any); ok {
		if vals, ok := deleteMap["category"]; ok {
			toRemove, err := extractStringValues(vals)
			if err != nil {
				return "", err
			}
			fm.Tags = removeStrings(fm.Tags, toRemove)
		}
	} else if deleteSlice, ok := updates.Delete.([]any); ok {
		for _, key := range deleteSlice {
			if k, ok := key.(string); ok {
				if k == "category" {
					fm.Tags = nil
				}
			}
		}
	}

	fullContent, err := rebuildPost(fm, content)
	if err != nil {
		return "", fmt.Errorf("rebuild post: %w", err)
	}
	return fullContent, nil
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
		rel, err := filepath.Rel(j.repo.path, f)
		if err != nil {
			continue
		}
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
	if r.Method != http.MethodPost {
		return ""
	}
	// indielib removes access_token from Form when parsing a create. PostForm
	// retains the body values, and avoids accepting tokens from query strings.
	if token := r.PostFormValue("access_token"); token != "" {
		return token
	}
	return ""
}

func extractContent(props map[string][]any) string {
	if vals, ok := props["content"]; ok && len(vals) > 0 {
		s, _ := contentValueToString(vals[0])
		return s
	}
	return ""
}

// contentValueToString converts a single Micropub content value to the post
// body. It accepts a plain string, {"html": "..."} (e.g. Sunlit) and
// {"value": "..."}; ok is false for any other shape.
func contentValueToString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case map[string]any:
		if s, ok := v["html"].(string); ok {
			return s, true
		}
		if s, ok := v["value"].(string); ok {
			return s, true
		}
	}
	return "", false
}

// jekyllIncludeValue renders s as a double-quoted parameter value for a
// Jekyll {% include %} tag.
//
// Jekyll's include tag (lib/jekyll/tags/include.rb) matches quoted values with
// "([^"\\]*(?:\\.[^"\\]*)*)" and then unescapes only \" -> ". Every other
// backslash sequence is kept verbatim, so a backslash adjacent to a quote (or
// at the end of the value) cannot be represented and would break the tag.
// Therefore:
//   - backslashes are replaced with U+FF3C FULLWIDTH REVERSE SOLIDUS,
//   - runs of whitespace (including newlines) collapse to a single space,
//   - Liquid delimiters ({%, %}, {{, }}) are broken up so the value can
//     neither end the tag early nor be treated as a variable include,
//   - " is escaped as \".
func jekyllIncludeValue(s string) string {
	s = strings.ReplaceAll(s, `\`, "\uFF3C")
	s = strings.Join(strings.Fields(s), " ")
	s = neutralizeLiquid(s)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// neutralizeLiquid inserts a space into Liquid tag/output delimiters so text
// written into a post body is not interpreted by Liquid.
func neutralizeLiquid(s string) string {
	replacer := strings.NewReplacer("{%", "{ %", "%}", "% }", "{{", "{ {", "}}", "} }")
	for {
		next := replacer.Replace(s)
		if next == s {
			return s
		}
		s = next
	}
}

// neutralizeLiquidURL percent-encodes braces so a URL cannot contain Liquid
// delimiters.
func neutralizeLiquidURL(s string) string {
	return strings.NewReplacer("{", "%7B", "}", "%7D").Replace(s)
}

// isSafeIncludeBaseName reports whether a base image name can be passed to
// responsive_image.html, which interpolates it unescaped into URLs/attributes.
func isSafeIncludeBaseName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return false
		}
	}
	return true
}

// isSafeThumbhash reports whether th looks like base64 ThumbHash data.
func isSafeThumbhash(th string) bool {
	for _, r := range th {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func extractStringSlice(props map[string][]any, key string) []string {
	vals, ok := props[key]
	if !ok {
		return nil
	}
	return toStringSlice(vals)
}

type photoReference struct {
	URL string
	Alt string
}

func extractPhotos(props, commands map[string][]any) []photoReference {
	vals, ok := props["photo"]
	if !ok {
		return nil
	}

	commandAlts := extractStringSlice(commands, "photo-alt")
	photos := make([]photoReference, 0, len(vals))
	for index, value := range vals {
		photo := photoReference{}

		switch entry := value.(type) {
		case string:
			photo.URL = entry
		case map[string]any:
			if urlValue, ok := entry["value"].(string); ok {
				photo.URL = urlValue
			} else if urlValue, ok := entry["url"].(string); ok {
				photo.URL = urlValue
			}
			if altValue, ok := entry["alt"].(string); ok {
				photo.Alt = altValue
			}
		}

		if photo.URL == "" {
			continue
		}
		if photo.Alt == "" && index < len(commandAlts) {
			photo.Alt = commandAlts[index]
		}

		photos = append(photos, photo)
	}

	return photos
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
	name, ok := managedPhotoRelativePath(photoURL, baseURL)
	if !ok {
		return "micro/"
	}

	decodedName, err := url.PathUnescape(name)
	if err == nil {
		name = decodedName
	}

	if ext := filepath.Ext(name); ext != "" {
		switch strings.ToLower(ext) {
		case ".jpg", ".jpeg", ".png":
			name = name[:len(name)-len(ext)]
		}
	}

	for _, suffix := range []string{"-small", "-medium", "-large", "-xlarge"} {
		name = strings.TrimSuffix(name, suffix)
	}

	return "micro/" + name
}

func parseTokenVerificationResponse(body []byte) (tokenVerificationResponse, error) {
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "" {
		return tokenVerificationResponse{}, fmt.Errorf("empty token response")
	}

	var tokenResp tokenVerificationResponse
	if err := json.Unmarshal(body, &tokenResp); err == nil {
		if tokenResp.Me != "" || tokenResp.Scope != "" || tokenResp.ClientID != "" {
			return tokenResp, nil
		}
	}

	values, err := url.ParseQuery(trimmedBody)
	if err != nil {
		return tokenVerificationResponse{}, err
	}

	return tokenVerificationResponse{
		Me:       values.Get("me"),
		Scope:    values.Get("scope"),
		ClientID: values.Get("client_id"),
	}, nil
}

func verifyProfileURLMatch(actual, expected string) error {
	actualURL, err := normalizeProfileURL(actual)
	if err != nil {
		return fmt.Errorf("normalize token 'me' URL %q: %w", actual, err)
	}

	expectedURL, err := normalizeProfileURL(expected)
	if err != nil {
		return fmt.Errorf("normalize site URL %q: %w", expected, err)
	}

	if actualURL != expectedURL {
		return fmt.Errorf("token 'me' mismatch: got %q, want %q", actualURL, expectedURL)
	}

	return nil
}

func normalizeProfileURL(raw string) (string, error) {
	canonical := canonicalizeURL(raw)
	if err := indieauth.IsValidProfileURL(canonical); err != nil {
		return "", err
	}

	parsed, err := url.Parse(canonical)
	if err != nil {
		return "", err
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""

	return parsed.String(), nil
}

func parseComparableURL(raw string) (*url.URL, error) {
	raw = canonicalizeURL(raw)

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	if parsed.Path == "/" {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}

	return parsed, nil
}

func canonicalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if parsed.Scheme == "" {
		return raw
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	raw = parsed.String()
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return indieauth.CanonicalizeURL(raw)
	}

	return raw
}

func requestedPublishedStatus(req *micropub.Request, honorCreatePostStatus bool) bool {
	if !honorCreatePostStatus {
		return true
	}

	statuses := extractStringSlice(req.Properties, "post-status")
	if len(statuses) == 0 {
		statuses = extractStringSlice(req.Commands, "post-status")
	}
	if len(statuses) == 0 {
		return true
	}
	return !strings.EqualFold(statuses[0], "draft")
}

func isManagedPhotoURL(photoURL, baseURL string) bool {
	_, ok := managedPhotoRelativePath(photoURL, baseURL)
	return ok
}

func managedPhotoRelativePath(photoURL, baseURL string) (string, bool) {
	photo, err := parseComparableURL(photoURL)
	if err != nil {
		return "", false
	}
	base, err := parseComparableURL(baseURL)
	if err != nil {
		return "", false
	}

	if photo.Host != base.Host {
		return "", false
	}

	photoPath := strings.TrimSuffix(photo.EscapedPath(), "/")
	basePath := strings.TrimSuffix(base.EscapedPath(), "/")
	if basePath == "" {
		basePath = "/"
	}
	if !strings.HasPrefix(photoPath, basePath+"/") {
		return "", false
	}

	return strings.TrimPrefix(photoPath, basePath+"/"), true
}

func extractStringValues(value any) ([]string, error) {
	switch typedValue := value.(type) {
	case string:
		return []string{typedValue}, nil
	case []string:
		return append([]string(nil), typedValue...), nil
	case []any:
		result := make([]string, 0, len(typedValue))
		for _, item := range typedValue {
			stringValue, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%w: expected string values in update delete request", micropub.ErrBadRequest)
			}
			result = append(result, stringValue)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%w: invalid update delete value type %T", micropub.ErrBadRequest, value)
	}
}

// urlToFilename converts a post URL to a file path in the repo.
// URL format: https://chenna.me/notes/2026/04/03/143000/
// File format: _micros/2026/2026-04-03-143000.md
//
// The URL must be on the site's host, under <siteURL>/notes/, and consist of
// exactly a 4-digit year, 2-digit month, 2-digit day and 6-digit time.
func (j *jekyllMicropub) urlToFilename(postURL string) (string, error) {
	invalid := fmt.Errorf("%w: invalid post URL: %s", micropub.ErrBadRequest, postURL)

	post, err := parseComparableURL(postURL)
	if err != nil {
		return "", invalid
	}
	site, err := parseComparableURL(j.siteURL)
	if err != nil {
		return "", fmt.Errorf("invalid site URL %q: %w", j.siteURL, err)
	}
	if post.Host != site.Host || (post.Scheme != "http" && post.Scheme != "https") {
		return "", invalid
	}

	rest, ok := strings.CutPrefix(post.Path, site.Path+"/notes/")
	if !ok || post.RawPath != "" {
		// RawPath is set when the path contains escapes (e.g. %2F) whose
		// decoded form differs; real post URLs never need them.
		return "", invalid
	}

	// Expected: "2026/04/03/143000"
	parts := strings.Split(rest, "/")
	if len(parts) != 4 {
		return "", invalid
	}
	for i, wantLen := range []int{4, 2, 2, 6} {
		if !isASCIIDigits(parts[i], wantLen) {
			return "", invalid
		}
	}

	year, month, day, hms := parts[0], parts[1], parts[2], parts[3]
	return fmt.Sprintf("_micros/%s/%s-%s-%s-%s.md", year, year, month, day, hms), nil
}

func isASCIIDigits(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// filenameToURL converts a file path to a post URL.
// File format: _micros/2026/2026-04-03-143000.md
// URL format: https://chenna.me/notes/2026/04/03/143000/
func (j *jekyllMicropub) filenameToURL(filename string) string {
	name := strings.TrimPrefix(filename, "_micros/")
	name = strings.TrimSuffix(name, ".md")

	// Expected: "2026/2026-04-03-143000"
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return j.siteURL + "/notes/"
	}

	// Parse "2026-04-03-143000" → year, month, day, time
	dateParts := strings.SplitN(parts[1], "-", 4)
	if len(dateParts) != 4 {
		return j.siteURL + "/notes/"
	}

	return fmt.Sprintf("%s/notes/%s/%s/%s/%s/", j.siteURL, dateParts[0], dateParts[1], dateParts[2], dateParts[3])
}

// postToMf2 converts a Jekyll post file to microformats2 properties.
func postToMf2(data, url string) map[string]any {
	fm, content, err := parseFrontMatter(data)
	if err != nil {
		log.Printf("error parsing front matter for %s: %v", url, err)
		fm = jekyllFrontMatter{}
		content = data
	}

	props := map[string]any{
		"url": []any{url},
	}

	if content != "" {
		props["content"] = []any{content}
	}

	if fm.Date != "" {
		props["published"] = []any{fm.Date}
	}

	if len(fm.Tags) > 0 {
		cats := make([]any, len(fm.Tags))
		for i, t := range fm.Tags {
			cats[i] = t
		}
		props["category"] = cats
	}

	if fm.Published != nil && !*fm.Published {
		props["post-status"] = []any{"draft"}
	}

	return map[string]any{
		"type":       []string{"h-entry"},
		"properties": props,
	}
}

func containsStringFold(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
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
