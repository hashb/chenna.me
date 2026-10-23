package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.hacdias.com/indielib/micropub"
)

func newTestMicropub(t *testing.T) (*jekyllMicropub, tempGitRepoPaths) {
	t.Helper()

	repoPaths := newTempGitRepo(t)
	repo, err := newGitRepo(repoPaths.localDir)
	if err != nil {
		t.Fatalf("newGitRepo: %v", err)
	}
	return &jekyllMicropub{
		repo:         repo,
		imageBaseURL: "//i.chenna.me/photos/prod/opt/micro",
		siteURL:      "https://chenna.me",
	}, repoPaths
}

func readRepoFile(t *testing.T, repoPaths tempGitRepoPaths, filename string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoPaths.localDir, filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return string(data)
}

func createRequest(content, published string) *micropub.Request {
	return &micropub.Request{
		Properties: map[string][]any{
			"content":   {content},
			"published": {published},
		},
		Commands: map[string][]any{},
	}
}

// --- Finding 1: Create must never overwrite an existing post ---

func TestCreateWithSameDateDoesNotOverwriteExistingPost(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	first, err := impl.Create(createRequest("first post", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatalf("Create (first): %v", err)
	}
	second, err := impl.Create(createRequest("second post", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatalf("Create (second): %v", err)
	}

	if first != "https://chenna.me/notes/2026/04/03/080000/" {
		t.Fatalf("first location = %q", first)
	}
	if second != "https://chenna.me/notes/2026/04/03/080001/" {
		t.Fatalf("second location = %q, want the date bumped by one second", second)
	}

	if body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080000.md"); !strings.Contains(body, "first post") {
		t.Fatalf("first post was overwritten:\n%s", body)
	}
	body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080001.md")
	if !strings.Contains(body, "second post") {
		t.Fatalf("second post missing content:\n%s", body)
	}
	// The permalink is derived from the front matter date, so it must match the
	// bumped filename/URL.
	if !strings.Contains(body, "date: 2026-04-03 08:00:01 +0000") {
		t.Fatalf("second post front matter date not bumped:\n%s", body)
	}
}

func TestCreateRetriesWhenRemoteAlreadyHasPost(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	// The local checkout does not have this file yet, so only the O_EXCL check
	// after syncing with the remote can detect the collision.
	pushFromPeer(t, repoPaths.remoteDir, "_micros/2026/2026-04-03-080000.md", "peer post\n")

	location, err := impl.Create(createRequest("local post", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if location != "https://chenna.me/notes/2026/04/03/080001/" {
		t.Fatalf("location = %q, want bumped URL", location)
	}
	if body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080000.md"); body != "peer post\n" {
		t.Fatalf("peer post was overwritten: %q", body)
	}
	if body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080001.md"); !strings.Contains(body, "local post") {
		t.Fatalf("local post missing content:\n%s", body)
	}
	if got := runGit(t, repoPaths.localDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want empty", got)
	}
}

// --- Finding 2: Update must not write Go map syntax ---

func TestUpdateReplaceContentAcceptsSupportedShapes(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	location, err := impl.Create(createRequest("original", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	filename := "_micros/2026/2026-04-03-080000.md"

	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "plain text", "plain text"},
		{"html", map[string]any{"html": "<p>html body</p>"}, "<p>html body</p>"},
		{"value", map[string]any{"value": "value body"}, "value body"},
	}
	for _, tc := range cases {
		req := &micropub.Request{
			URL: location,
			Updates: micropub.RequestUpdate{
				Replace: map[string][]any{"content": {tc.value}},
			},
		}
		if _, err := impl.Update(req); err != nil {
			t.Fatalf("%s: Update: %v", tc.name, err)
		}
		body := readRepoFile(t, repoPaths, filename)
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s: body missing %q:\n%s", tc.name, tc.want, body)
		}
		if strings.Contains(body, "map[") {
			t.Fatalf("%s: body contains Go map syntax:\n%s", tc.name, body)
		}
	}
}

func TestUpdateRejectsUnsupportedValues(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	location, err := impl.Create(createRequest("original", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	filename := "_micros/2026/2026-04-03-080000.md"
	before := readRepoFile(t, repoPaths, filename)

	cases := map[string]map[string][]any{
		"content number":      {"content": {42}},
		"content unknown map": {"content": {map[string]any{"text": "nope"}}},
		"content html number": {"content": {map[string]any{"html": 1}}},
		"post-status map":     {"post-status": {map[string]any{"value": "draft"}}},
	}
	for name, replace := range cases {
		req := &micropub.Request{
			URL:     location,
			Updates: micropub.RequestUpdate{Replace: replace},
		}
		if _, err := impl.Update(req); !errors.Is(err, micropub.ErrBadRequest) {
			t.Fatalf("%s: Update error = %v, want ErrBadRequest", name, err)
		}
		if after := readRepoFile(t, repoPaths, filename); after != before {
			t.Fatalf("%s: post changed after rejected update:\n%s", name, after)
		}
	}
}

func TestContentValueToString(t *testing.T) {
	t.Parallel()

	if got, ok := contentValueToString(map[string]any{"html": "<b>x</b>", "value": "x"}); !ok || got != "<b>x</b>" {
		t.Fatalf("html preferred over value: got %q, %v", got, ok)
	}
	if _, ok := contentValueToString([]any{"x"}); ok {
		t.Fatal("contentValueToString accepted a slice")
	}
}

// --- Finding 3: urlToFilename validation ---

func TestURLToFilenameAcceptsPostURLs(t *testing.T) {
	t.Parallel()

	impl := &jekyllMicropub{siteURL: "https://chenna.me"}
	for _, raw := range []string{
		"https://chenna.me/notes/2026/04/03/143000/",
		"https://chenna.me/notes/2026/04/03/143000",
		"https://CHENNA.me/notes/2026/04/03/143000/",
		"https://chenna.me/notes/2026/04/03/143000/?utm=x#frag",
	} {
		got, err := impl.urlToFilename(raw)
		if err != nil {
			t.Fatalf("urlToFilename(%q): %v", raw, err)
		}
		if got != "_micros/2026/2026-04-03-143000.md" {
			t.Fatalf("urlToFilename(%q) = %q", raw, got)
		}
	}
}

func TestURLToFilenameRejectsInvalidURLs(t *testing.T) {
	t.Parallel()

	impl := &jekyllMicropub{siteURL: "https://chenna.me"}
	for _, raw := range []string{
		"",
		"/notes/2026/04/03/143000/",
		"https://chenna.me/micro/2026/04/03/143000/",
		"https://chenna.me/2026/04/03/143000/",
		"https://attacker.example/notes/2026/04/03/143000/",
		"https://chenna.me.attacker.example/notes/2026/04/03/143000/",
		"ftp://chenna.me/notes/2026/04/03/143000/",
		"https://chenna.me/notes/../../etc/passwd/",
		"https://chenna.me/notes/2026/../../../x/",
		"https://chenna.me/notes/2026/04/03/../../../../README/",
		"https://chenna.me/notes/2026/04/03/%2e%2e/",
		"https://chenna.me/notes/2026/04/03/14%2F3000/",
		"https://chenna.me/notes/..%2F..%2F/04/03/143000/",
		"https://chenna.me/notes/2026/4/03/143000/",
		"https://chenna.me/notes/2026/04/03/1430/",
		"https://chenna.me/notes/2026/04/03/14300a/",
		"https://chenna.me/notes/2026/04/03/143000/extra/",
		"https://chenna.me/notes/20266/04/03/143000/",
		"https://chenna.me/other/notes/2026/04/03/143000/",
	} {
		got, err := impl.urlToFilename(raw)
		if !errors.Is(err, micropub.ErrBadRequest) {
			t.Fatalf("urlToFilename(%q) = %q, %v; want ErrBadRequest", raw, got, err)
		}
	}
}

func TestURLToFilenameRoundTripsFilenameToURL(t *testing.T) {
	t.Parallel()

	impl := &jekyllMicropub{siteURL: "https://chenna.me"}
	filename := "_micros/2026/2026-04-03-143000.md"
	got, err := impl.urlToFilename(impl.filenameToURL(filename))
	if err != nil || got != filename {
		t.Fatalf("round trip = %q, %v; want %q", got, err, filename)
	}
}

// --- Finding 4: alt text escaping ---

// jekyllIncludeParam mirrors Jekyll's include tag parameter parsing
// (Jekyll::Tags::IncludeTag::VALID_SYNTAX / parse_params): a double-quoted
// value matches "([^"\\]*(?:\\.[^"\\]*)*)" and only \" is unescaped.
var jekyllIncludeParam = regexp.MustCompile(`([\w-]+)\s*=\s*(?:"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)'|([\w.-]+))`)
var jekyllIncludeFullSyntax = regexp.MustCompile(`\A\s*(?:` + jekyllIncludeParam.String() + `(?:\s|\z)\s*)*\z`)

// parseIncludeTag extracts the include tag from body the way Liquid tokenizes
// it (first "{%" up to the first "%}") and returns its parameters as Jekyll
// would see them.
func parseIncludeTag(t *testing.T, body string) map[string]string {
	t.Helper()

	start := strings.Index(body, "{% include responsive_image.html ")
	if start < 0 {
		t.Fatalf("no include tag in body:\n%s", body)
	}
	rest := body[start+len("{% include responsive_image.html "):]
	end := strings.Index(rest, "%}")
	if end < 0 {
		t.Fatalf("unterminated include tag:\n%s", body)
	}
	markup := rest[:end]
	if strings.Contains(rest[end+2:], "%}") {
		t.Fatalf("stray %%} after include tag (tag terminated early?):\n%s", body)
	}
	if strings.Contains(markup, "{{") || strings.Contains(markup, "{%") || strings.Contains(markup, "\n") {
		t.Fatalf("include markup contains Liquid delimiters or newlines: %q", markup)
	}
	if !jekyllIncludeFullSyntax.MatchString(markup) {
		t.Fatalf("include markup %q does not satisfy Jekyll's FULL_VALID_SYNTAX", markup)
	}

	params := map[string]string{}
	for _, m := range jekyllIncludeParam.FindAllStringSubmatch(markup, -1) {
		switch {
		case m[2] != "" || strings.HasPrefix(m[0][len(m[1]):], `="`):
			params[m[1]] = strings.ReplaceAll(m[2], `\"`, `"`)
		default:
			params[m[1]] = m[4]
		}
	}
	return params
}

func createWithManagedPhotoAlt(t *testing.T, alt string) string {
	t.Helper()

	impl, repoPaths := newTestMicropub(t)
	req := &micropub.Request{
		Properties: map[string][]any{
			"published": {"2026-04-03T08:00:00Z"},
			"photo": {map[string]any{
				"value": "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg",
				"alt":   alt,
			}},
		},
		Commands: map[string][]any{},
	}
	if _, err := impl.Create(req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080000.md")
}

func TestCreateIncludeAltSurvivesJekyllParsing(t *testing.T) {
	cases := []struct {
		name string
		alt  string
		want string
	}{
		{"plain", "Sunrise over the bay", "Sunrise over the bay"},
		{"quotes", `She said "hi" there`, `She said "hi" there`},
		{"trailing quote", `ends with "`, `ends with "`},
		{"backslashes", `C:\path\ and \"x\" \`, "C:\uFF3Cpath\uFF3C and \uFF3C\"x\uFF3C\" \uFF3C"},
		{"newlines", "line one\nline two\r\n\tline three  ", "line one line two line three"},
		{"liquid tag end", `before %} {% raw %} after`, `before % } { % raw % } after`},
		{"liquid output", `{{ site.title }}}}`, `{ { site.title } } } }`},
		{"single quotes", `it's`, `it's`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := createWithManagedPhotoAlt(t, tc.alt)
			params := parseIncludeTag(t, body)
			if params["alt"] != tc.want {
				t.Fatalf("alt = %q, want %q\nbody:\n%s", params["alt"], tc.want, body)
			}
			if params["base_image_name"] != "micro/2026-04-03-080000-1" {
				t.Fatalf("base_image_name = %q", params["base_image_name"])
			}
			if params["width"] != "1920" || params["height"] != "auto" {
				t.Fatalf("width/height = %q/%q", params["width"], params["height"])
			}
		})
	}
}

func TestCreateFallbackImageEscapesAttributes(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)
	req := &micropub.Request{
		Properties: map[string][]any{
			"published": {"2026-04-03T08:00:00Z"},
			"photo": {map[string]any{
				"value": `https://example.com/a.jpg?x="><script>`,
				"alt":   "a \"quoted\" <b>alt</b> & \\ back\nslash {{ x }}",
			}},
		},
		Commands: map[string][]any{},
	}
	if _, err := impl.Create(req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080000.md")

	want := `<img src="https://example.com/a.jpg?x=&#34;&gt;&lt;script&gt;" alt="a &#34;quoted&#34; &lt;b&gt;alt&lt;/b&gt; &amp; \ back slash { { x } }" style="max-width: 100%; height: auto;">`
	if !strings.Contains(body, want) {
		t.Fatalf("fallback img not escaped as expected.\nwant: %s\nbody:\n%s", want, body)
	}
}

func TestCreateFallsBackForUnsafeManagedBaseName(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)
	req := &micropub.Request{
		Properties: map[string][]any{
			"published": {"2026-04-03T08:00:00Z"},
			"photo":     {"https://i.chenna.me/photos/prod/opt/micro/a%22b-xlarge.jpg"},
		},
		Commands: map[string][]any{},
	}
	if _, err := impl.Create(req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := readRepoFile(t, repoPaths, "_micros/2026/2026-04-03-080000.md")
	if strings.Contains(body, "{% include") {
		t.Fatalf("unsafe base name passed to include:\n%s", body)
	}
	if !strings.Contains(body, `<img src="https://i.chenna.me/photos/prod/opt/micro/a%22b-xlarge.jpg"`) {
		t.Fatalf("missing fallback img:\n%s", body)
	}
}

func TestJekyllIncludeValue(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                 `""`,
		"plain":            `"plain"`,
		`a"b`:              `"a\"b"`,
		"a\\b":             "\"a\uFF3Cb\"",
		"x\n\ny":           `"x y"`,
		"%}":               `"% }"`,
		"{%%}":             `"{ %% }"`,
		"{{{":              `"{ { {"`,
		"}}}":              `"} } }"`,
		"  leading/trail ": `"leading/trail"`,
	}
	for in, want := range cases {
		if got := jekyllIncludeValue(in); got != want {
			t.Fatalf("jekyllIncludeValue(%q) = %s, want %s", in, got, want)
		}
	}
}

// --- Finding 5: media caches are drained and pruned ---

func TestCreateConsumesMediaCacheEntries(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	photoURL := "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg"
	exif := time.Date(2025, 12, 24, 18, 30, 15, 0, time.UTC)
	impl.rememberMediaMetadata(photoURL, "abc+/=", &exif)

	location, err := impl.Create(&micropub.Request{
		Properties: map[string][]any{"photo": {photoURL}},
		Commands:   map[string][]any{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if location != "https://chenna.me/notes/2025/12/24/183015/" {
		t.Fatalf("location = %q, want EXIF-derived URL", location)
	}
	body := readRepoFile(t, repoPaths, "_micros/2025/2025-12-24-183015.md")
	if !strings.Contains(body, `thumbhash="abc+/="`) {
		t.Fatalf("thumbhash missing from include:\n%s", body)
	}

	if _, ok := impl.thumbhashCache.Load(photoURL); ok {
		t.Fatal("thumbhash cache entry not removed after Create")
	}
	if _, ok := impl.exifDateCache.Load(photoURL); ok {
		t.Fatal("EXIF date cache entry not removed after Create")
	}
}

func TestCreateAcceptsRawMediaCacheValues(t *testing.T) {
	impl, repoPaths := newTestMicropub(t)

	// media.go may store raw values (no timestamp wrapper).
	photoURL := "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg"
	impl.thumbhashCache.Store(photoURL, "rawhash")
	impl.exifDateCache.Store(photoURL, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))

	if _, err := impl.Create(&micropub.Request{
		Properties: map[string][]any{"photo": {photoURL}},
		Commands:   map[string][]any{},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := readRepoFile(t, repoPaths, "_micros/2025/2025-01-02-030405.md")
	if !strings.Contains(body, `thumbhash="rawhash"`) {
		t.Fatalf("thumbhash missing from include:\n%s", body)
	}
	if _, ok := impl.thumbhashCache.Load(photoURL); ok {
		t.Fatal("raw thumbhash cache entry not removed after Create")
	}
}

func TestPruneMediaCachesDropsStaleEntries(t *testing.T) {
	t.Parallel()

	impl := &jekyllMicropub{}
	now := time.Now()
	impl.thumbhashCache.Store("stale", mediaCacheEntry{value: "x", storedAt: now.Add(-25 * time.Hour)})
	impl.thumbhashCache.Store("fresh", mediaCacheEntry{value: "y", storedAt: now.Add(-time.Hour)})
	impl.exifDateCache.Store("stale", mediaCacheEntry{value: now, storedAt: now.Add(-25 * time.Hour)})

	impl.pruneMediaCaches(now)

	if _, ok := impl.thumbhashCache.Load("stale"); ok {
		t.Fatal("stale thumbhash entry not pruned")
	}
	if _, ok := impl.exifDateCache.Load("stale"); ok {
		t.Fatal("stale EXIF entry not pruned")
	}
	if _, ok := impl.thumbhashCache.Load("fresh"); !ok {
		t.Fatal("fresh thumbhash entry pruned")
	}
}
