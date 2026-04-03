package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"go.hacdias.com/indielib/micropub"
)

func main() {
	port := getenv("PORT", "8080")
	repoPath := getenv("REPO_PATH", "/data/chenna.me")
	gcsBucket := getenv("GCS_BUCKET", "")
	gcsPrefix := getenv("GCS_PREFIX", "photos/prod/opt/micro")
	imageBaseURL := getenv("IMAGE_BASE_URL", "//i.chenna.me/photos/prod/opt/micro")
	siteURL := getenv("SITE_URL", "https://chenna.me")
	tokenEndpoint := getenv("TOKEN_ENDPOINT", "https://tokens.indieauth.com/token")
	allowedOrigins := parseOrigins(getenv("ALLOWED_ORIGINS", ""))

	repo, err := newGitRepo(repoPath)
	if err != nil {
		log.Fatalf("failed to initialize git repo: %v", err)
	}

	var gcsClient *gcsUploader
	if gcsBucket != "" {
		client, err := storage.NewClient(context.Background())
		if err != nil {
			log.Fatalf("failed to create GCS client: %v", err)
		}
		defer client.Close()
		gcsClient = &gcsUploader{
			client: client,
			bucket: gcsBucket,
			prefix: gcsPrefix,
		}
	} else {
		log.Println("WARNING: GCS_BUCKET not set, media uploads will be disabled")
	}

	impl := &jekyllMicropub{
		repo:          repo,
		gcs:           gcsClient,
		imageBaseURL:  imageBaseURL,
		siteURL:       siteURL,
		tokenEndpoint: tokenEndpoint,
	}

	mux := http.NewServeMux()
	mux.Handle("/micropub", micropub.NewHandler(impl,
		micropub.WithMediaEndpoint(siteURL+"/media"),
		micropub.WithGetCategories(impl.getCategories),
		micropub.WithGetPostStatuses(func() []string {
			return []string{"published", "draft"}
		}),
	))
	mux.Handle("/media", micropub.NewMediaHandler(
		impl.uploadMedia, impl.hasScope,
	))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	handler := corsMiddleware(mux, allowedOrigins)

	log.Printf("starting micropub server on :%s", port)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseOrigins(env string) []string {
	defaults := []string{"https://chenna.me", "http://localhost:4000"}
	if env == "" {
		return defaults
	}
	var origins []string
	for _, o := range strings.Split(env, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return defaults
	}
	return origins
}

func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")
		if originSet[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
