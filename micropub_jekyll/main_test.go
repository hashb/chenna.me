package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	t.Setenv("EXISTING_VALUE", "keep-me")

	envPath := filepath.Join(t.TempDir(), ".env")
	envContents := `# comment
export PORT=9999
REPO_PATH=/srv/chenna.me
ENDPOINT_URL=https://micropub.chenna.me
ALLOWED_ORIGINS="https://chenna.me,https://staging.chenna.me"
EXISTING_VALUE=replace-me
`

	if err := os.WriteFile(envPath, []byte(envContents), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadEnvFile(envPath); err != nil {
		t.Fatalf("load env file: %v", err)
	}

	if got := os.Getenv("PORT"); got != "9999" {
		t.Fatalf("PORT = %q, want %q", got, "9999")
	}
	if got := os.Getenv("REPO_PATH"); got != "/srv/chenna.me" {
		t.Fatalf("REPO_PATH = %q, want %q", got, "/srv/chenna.me")
	}
	if got := os.Getenv("ENDPOINT_URL"); got != "https://micropub.chenna.me" {
		t.Fatalf("ENDPOINT_URL = %q, want %q", got, "https://micropub.chenna.me")
	}
	if got := os.Getenv("ALLOWED_ORIGINS"); got != "https://chenna.me,https://staging.chenna.me" {
		t.Fatalf("ALLOWED_ORIGINS = %q", got)
	}
	if got := os.Getenv("EXISTING_VALUE"); got != "keep-me" {
		t.Fatalf("EXISTING_VALUE = %q, want %q", got, "keep-me")
	}
}
