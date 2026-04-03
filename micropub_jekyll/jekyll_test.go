package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.hacdias.com/indielib/micropub"
)

func TestHasScopeAcceptsFormEncodedVerificationResponse(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		_, _ = w.Write([]byte("me=https%3A%2F%2Fchenna.me%2F&scope=create+media"))
	}))
	defer tokenServer.Close()

	impl := &jekyllMicropub{
		siteURL:       "https://chenna.me",
		tokenEndpoint: tokenServer.URL,
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	if !impl.HasScope(req, "media") {
		t.Fatal("HasScope returned false for a valid form-encoded token response")
	}
}

func TestHasScopeRejectsSpoofedMePrefix(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"me":"https://chenna.me.attacker.example/","scope":"create media"}`))
	}))
	defer tokenServer.Close()

	impl := &jekyllMicropub{
		siteURL:       "https://chenna.me",
		tokenEndpoint: tokenServer.URL,
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	if impl.HasScope(req, "create") {
		t.Fatal("HasScope accepted a spoofed me URL with a matching prefix")
	}
}

func TestHasScopeRejectsMismatchedQueryInMeURL(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"me":"https://chenna.me/?via=attacker","scope":"create media"}`))
	}))
	defer tokenServer.Close()

	impl := &jekyllMicropub{
		siteURL:       "https://chenna.me",
		tokenEndpoint: tokenServer.URL,
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	if impl.HasScope(req, "create") {
		t.Fatal("HasScope accepted a token me URL with a mismatched query string")
	}
}

func TestHasScopeRejectsFragmentedMeURL(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"me":"https://chenna.me/#me","scope":"create media"}`))
	}))
	defer tokenServer.Close()

	impl := &jekyllMicropub{
		siteURL:       "https://chenna.me",
		tokenEndpoint: tokenServer.URL,
	}

	req := httptest.NewRequest(http.MethodPost, "/micropub", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	if impl.HasScope(req, "create") {
		t.Fatal("HasScope accepted a token me URL with a fragment")
	}
}

func TestRequestedPublishedStatusReadsCreateProperty(t *testing.T) {
	t.Parallel()

	req := &micropub.Request{
		Properties: map[string][]any{
			"post-status": {"draft"},
		},
		Commands: map[string][]any{},
	}

	if requestedPublishedStatus(req) {
		t.Fatal("requestedPublishedStatus returned published for a draft create request")
	}
}

func TestManagedPhotoURLRequiresRealBasePrefix(t *testing.T) {
	t.Parallel()

	baseURL := "//i.chenna.me/photos/prod/opt/micro"
	goodURL := "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-143000-1-xlarge.jpg"
	badURL := "https://attacker.example//i.chenna.me/photos/prod/opt/micro/2026-04-03-143000-1-xlarge.jpg"

	if !isManagedPhotoURL(goodURL, baseURL) {
		t.Fatal("isManagedPhotoURL rejected a valid managed image URL")
	}
	if isManagedPhotoURL(badURL, baseURL) {
		t.Fatal("isManagedPhotoURL accepted a spoofed URL")
	}
	if got := extractBaseName(goodURL, baseURL); got != "micro/2026-04-03-143000-1" {
		t.Fatalf("extractBaseName = %q, want %q", got, "micro/2026-04-03-143000-1")
	}
}

func TestMediaObjectURLReturnsConcreteAsset(t *testing.T) {
	t.Parallel()

	got := mediaObjectURL("//i.chenna.me/photos/prod/opt/micro", "2026-04-03-143000-1")
	want := "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-143000-1-xlarge.jpg"
	if got != want {
		t.Fatalf("mediaObjectURL = %q, want %q", got, want)
	}
}

func TestExtractPhotosSupportsObjectValues(t *testing.T) {
	t.Parallel()

	photos := extractPhotos(map[string][]any{
		"photo": {
			map[string]any{
				"value": "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg",
				"alt":   "Sunrise over the bay",
			},
			"https://example.com/external.jpg",
		},
	}, map[string][]any{})

	if len(photos) != 2 {
		t.Fatalf("len(photos) = %d, want 2", len(photos))
	}
	if photos[0].URL != "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg" || photos[0].Alt != "Sunrise over the bay" {
		t.Fatalf("photos[0] = %#v", photos[0])
	}
	if photos[1].URL != "https://example.com/external.jpg" || photos[1].Alt != "" {
		t.Fatalf("photos[1] = %#v", photos[1])
	}
}

func TestCreateIncludesObjectValuedPhotoInPostBody(t *testing.T) {
	repoPaths := newTempGitRepo(t)

	repo, err := newGitRepo(repoPaths.localDir)
	if err != nil {
		t.Fatalf("newGitRepo: %v", err)
	}

	impl := &jekyllMicropub{
		repo:         repo,
		imageBaseURL: "//i.chenna.me/photos/prod/opt/micro",
		siteURL:      "https://chenna.me",
	}

	req := &micropub.Request{
		Properties: map[string][]any{
			"content":   {"hello"},
			"published": {"2026-04-03T08:00:00Z"},
			"photo": {
				map[string]any{
					"value": "https://i.chenna.me/photos/prod/opt/micro/2026-04-03-080000-1-xlarge.jpg",
					"alt":   "Sunrise over the bay",
				},
			},
		},
		Commands: map[string][]any{},
	}

	location, err := impl.Create(req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if location != "https://chenna.me/micro/2026/04/03/080000/" {
		t.Fatalf("location = %q, want %q", location, "https://chenna.me/micro/2026/04/03/080000/")
	}

	data, err := os.ReadFile(filepath.Join(repoPaths.localDir, "_micros/2026/2026-04-03-080000.md"))
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	body := string(data)
	if !strings.Contains(body, "hello") {
		t.Fatalf("post body missing content:\n%s", body)
	}
	if !strings.Contains(body, `{% include responsive_image.html base_image_name="micro/2026-04-03-080000-1" alt="Sunrise over the bay" width="1920" height="auto" thumbhash="" %}`) {
		t.Fatalf("post body missing responsive image include:\n%s", body)
	}
	if !strings.Contains(body, "- photos") {
		t.Fatalf("post body missing photos tag:\n%s", body)
	}
}

func TestRebuildPostPreservesMissingPublishedField(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"---",
		"layout: micro",
		"date: 2026-04-03 14:30:00 +0000",
		"tags:",
		"  - micro",
		"---",
		"",
		"hello, world",
	}, "\n")

	fm, content, err := parseFrontMatter(input)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	output, err := rebuildPost(fm, content)
	if err != nil {
		t.Fatalf("rebuildPost: %v", err)
	}
	if strings.Contains(output, "published:") {
		t.Fatalf("rebuildPost added a published field to a post that did not have one:\n%s", output)
	}

	mf2 := postToMf2(output, "https://chenna.me/micro/2026/04/03/143000/")
	props := mf2["properties"].(map[string]any)
	if _, ok := props["post-status"]; ok {
		t.Fatal("postToMf2 marked a post as draft when no published field existed")
	}
}

func TestParseFrontMatterRejectsInvalidYAML(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"---",
		"layout: micro",
		"tags: [micro",
		"---",
		"",
		"hello, world",
	}, "\n")

	_, _, err := parseFrontMatter(input)
	if err == nil {
		t.Fatal("parseFrontMatter succeeded for invalid YAML front matter")
	}
}

func TestExtractStringValuesRejectsInvalidDeleteShape(t *testing.T) {
	t.Parallel()

	_, err := extractStringValues(42)
	if !errors.Is(err, micropub.ErrBadRequest) {
		t.Fatalf("extractStringValues error = %v, want ErrBadRequest", err)
	}
}
