package main

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.hacdias.com/indielib/micropub"
)

func TestSourceQueriesRequireOwnerUpdateToken(t *testing.T) {
	impl, _ := newTestMicropub(t)
	impl.honorCreatePostStatus = true
	req := createRequest("private draft marker", "2026-04-03T08:00:00Z")
	req.Properties["post-status"] = []any{"draft"}
	location, err := impl.Create(req)
	if err != nil {
		t.Fatal(err)
	}
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer owner-update":
			io.WriteString(w, `{"me":"https://chenna.me/","scope":"update"}`)
		case "Bearer owner-create":
			io.WriteString(w, `{"me":"https://chenna.me/","scope":"create"}`)
		case "Bearer other-owner":
			io.WriteString(w, `{"me":"https://example.org/","scope":"update"}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer tokens.Close()
	impl.tokenEndpoint = tokens.URL
	handler := newMicropubHandler(impl)
	for _, endpoint := range []string{"/micropub?q=source", "/micropub?q=source&url=" + location} {
		for _, tc := range []struct {
			token  string
			status int
		}{{"", 401}, {"invalid", 403}, {"other-owner", 403}, {"owner-create", 403}, {"owner-update", 200}} {
			r := httptest.NewRequest(http.MethodGet, endpoint, nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%s token %q: status=%d body=%s", endpoint, tc.token, w.Code, w.Body.String())
			}
			if got := strings.Contains(w.Body.String(), "private draft marker"); got != (tc.status == 200) {
				t.Fatalf("%s token %q: unexpected draft visibility: %s", endpoint, tc.token, w.Body.String())
			}
		}
	}
	// Public discovery must remain usable before authentication.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/micropub?q=config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("config status = %d", w.Code)
	}
}

func TestUpdatePreservesRemoteEdit(t *testing.T) {
	impl, paths := newTestMicropub(t)
	location, err := impl.Create(createRequest("original body", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	name := "_micros/2026/2026-04-03-080000.md"
	newer := strings.ReplaceAll(readRepoFile(t, paths, name), "original body", "new remote body")
	pushFromPeer(t, paths.remoteDir, name, newer)
	_, err = impl.Update(&micropub.Request{URL: location, Updates: micropub.RequestUpdate{Add: map[string][]any{"category": {"tech"}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := readRepoFile(t, paths, name)
	if !strings.Contains(got, "new remote body") || !strings.Contains(got, "tech") {
		t.Fatalf("update lost content or category: %s", got)
	}
}

func TestConcurrentUpdatesPreserveBothChanges(t *testing.T) {
	impl, paths := newTestMicropub(t)
	location, err := impl.Create(createRequest("body", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, category := range []string{"tech", "links"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := impl.Update(&micropub.Request{URL: location, Updates: micropub.RequestUpdate{Add: map[string][]any{"category": {category}}}})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	fm, _, err := parseFrontMatter(readRepoFile(t, paths, "_micros/2026/2026-04-03-080000.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsStringFold(fm.Tags, "tech") || !containsStringFold(fm.Tags, "links") {
		t.Fatalf("concurrent updates lost categories: %v", fm.Tags)
	}
}

func TestUpdateDoesNotResurrectRemoteDeletion(t *testing.T) {
	impl, paths := newTestMicropub(t)
	location, err := impl.Create(createRequest("body", "2026-04-03T08:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	name := "_micros/2026/2026-04-03-080000.md"
	peer := filepath.Join(t.TempDir(), "peer")
	runGit(t, filepath.Dir(peer), "clone", paths.remoteDir, peer)
	runGit(t, peer, "rm", "--", name)
	runGit(t, peer, "commit", "-m", "delete remotely")
	runGit(t, peer, "push")
	_, err = impl.Update(&micropub.Request{URL: location, Updates: micropub.RequestUpdate{Add: map[string][]any{"category": {"tech"}}}})
	if !errors.Is(err, micropub.ErrNotFound) {
		t.Fatalf("Update error = %v, want not found", err)
	}
	if _, err := os.Stat(filepath.Join(paths.localDir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted post was recreated: %v", err)
	}
}

func TestCreateAcceptsFormToken(t *testing.T) {
	for _, multipartBody := range []bool{false, true} {
		name := "urlencoded"
		if multipartBody {
			name = "multipart text only"
		}
		t.Run(name, func(t *testing.T) {
			impl, paths := newTestMicropub(t)
			var calls atomic.Int32
			tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer valid-token" {
					t.Errorf("unexpected authorization")
				}
				io.WriteString(w, `{"me":"https://chenna.me/","scope":"create"}`)
			}))
			defer tokens.Close()
			impl.tokenEndpoint = tokens.URL
			body := "h=entry&content=hello&access_token=valid-token"
			contentType := "application/x-www-form-urlencoded"
			if multipartBody {
				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				for key, value := range map[string]string{"h": "entry", "content": "hello", "access_token": "valid-token"} {
					if err := mw.WriteField(key, value); err != nil {
						t.Fatal(err)
					}
				}
				if err := mw.Close(); err != nil {
					t.Fatal(err)
				}
				body, contentType = buf.String(), mw.FormDataContentType()
			}
			r := httptest.NewRequest(http.MethodPost, "/micropub", strings.NewReader(body))
			r.Header.Set("Content-Type", contentType)
			w := httptest.NewRecorder()
			newMicropubHandler(impl).ServeHTTP(w, r)
			if w.Code != http.StatusAccepted || calls.Load() != 1 {
				t.Fatalf("status=%d verification calls=%d body=%s", w.Code, calls.Load(), w.Body.String())
			}
			filename, err := impl.urlToFilename(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			post := readRepoFile(t, paths, filename)
			if !strings.Contains(post, "hello") || strings.Contains(post, "valid-token") {
				t.Fatalf("unexpected stored post: %s", post)
			}
		})
	}
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestMediaLimitsBeforeTokenParsing(t *testing.T) {
	var prefix bytes.Buffer
	mw := multipart.NewWriter(&prefix)
	if err := mw.WriteField("access_token", "invalid-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := mw.CreateFormFile("file", "large.bin"); err != nil {
		t.Fatal(err)
	}
	payload := io.LimitReader(zeroReader{}, maxMediaSize+(1<<20))
	counter := &countingReader{r: io.MultiReader(bytes.NewReader(prefix.Bytes()), payload, strings.NewReader("\r\n--"+mw.Boundary()+"--\r\n"))}
	// No token endpoint: oversized bodies must fail before token verification.
	impl := &jekyllMicropub{siteURL: "https://chenna.me"}
	r := httptest.NewRequest(http.MethodPost, "/media", counter)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	newMediaHandler(impl).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "request body too large") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	// MaxBytesReader reads one extra byte to detect an oversized stream.
	if counter.n > maxMediaSize+1 {
		t.Fatalf("read %d bytes beyond limit", counter.n)
	}
}

func TestMediaFormTokenAndTemporaryFileCleanup(t *testing.T) {
	var calls atomic.Int32
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer media-token" {
			t.Errorf("incorrect media authorization")
		}
		io.WriteString(w, `{"me":"https://chenna.me/","scope":"media"}`)
	}))
	defer tokens.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("access_token", "media-token"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("image bytes")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/media", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	// No GCS client: reaching this error demonstrates that authorization
	// succeeded, without making an external upload in the test.
	newMediaHandler(&jekyllMicropub{siteURL: "https://chenna.me", tokenEndpoint: tokens.URL}).ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError || calls.Load() != 1 || !strings.Contains(w.Body.String(), "media uploads are not configured") {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, calls.Load(), w.Body.String())
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) != 1 {
		t.Fatal("upload was not parsed")
	}
	file, err := r.MultipartForm.File["file"][0].Open()
	if err == nil {
		file.Close()
		t.Fatal("upload was retained after request (in memory or on disk)")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected cleanup error: %v", err)
	}
}

func TestCORSExposesLocationOnlyToAllowedOrigins(t *testing.T) {
	h := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://chenna.me/notes/new/")
		w.WriteHeader(http.StatusCreated)
	}), []string{"https://chenna.me"})
	for _, origin := range []string{"https://chenna.me", "https://other.example"} {
		r := httptest.NewRequest(http.MethodPost, "https://micropub.chenna.me/media", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if exposed := w.Header().Get("Access-Control-Expose-Headers") == "Location"; exposed != (origin == "https://chenna.me") {
			t.Fatalf("origin %s: unexpected exposed headers %v", origin, w.Header())
		}
	}
}

func TestMediaWithoutTokenOrMultipartBodyIsUnauthorized(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/media", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newMediaHandler(&jekyllMicropub{siteURL: "https://chenna.me"}).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"unauthorized"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
