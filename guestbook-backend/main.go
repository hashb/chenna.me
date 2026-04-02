package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/guestbook.db"
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Println("WARNING: ADMIN_TOKEN not set, admin endpoints will be disabled")
	}

	allowedOrigins := parseOrigins(os.Getenv("ALLOWED_ORIGINS"))

	db, err := initDB(dbPath)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
	defer db.Close()

	limiter := newRateLimiter(10, time.Hour)
	srv := newServer(db, adminToken, limiter)

	handler := corsMiddleware(srv, allowedOrigins)

	log.Printf("starting server on :%s", port)
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
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
