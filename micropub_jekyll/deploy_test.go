package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployRestartsAfterEnvOnlyChange(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// All privileged commands are replaced with local stubs. Run the real
	// script twice with an unchanged binary and unit, changing only .env.
	stubs := map[string]string{
		"go":         `printf 'same binary\n' > "$3"`,
		"sudo":       `exec "$@"`,
		"systemctl":  `printf '%s\n' "$*" >> "$DEPLOY_TEST_LOG"`,
		"journalctl": `exit 0`,
		"install": `
if [[ "$1" == "-d" ]]; then mkdir -p "$2"; exit; fi
while [[ "$1" == -* ]]; do
  case "$1" in
    -m) shift 2 ;;
    -D) shift ;;
    *) exit 1 ;;
  esac
done
mkdir -p "$(dirname "$2")"
cp "$1" "$2"
`,
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/bash\nset -eu\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	envFile := filepath.Join(root, ".env")
	logFile := filepath.Join(root, "commands.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DEPLOY_TEST_LOG", logFile)
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("BUILD_OUTPUT", filepath.Join(root, "build"))
	t.Setenv("INSTALL_PATH", filepath.Join(root, "installed"))
	t.Setenv("UNIT_PATH", filepath.Join(root, "service.unit"))
	t.Setenv("SERVICE_NAME", "micropub-test")
	t.Setenv("SERVICE_USER", "test")
	t.Setenv("SERVICE_GROUP", "test")
	t.Setenv("WORKING_DIR", root)
	t.Setenv("SETUP_PROXY", "0")
	for i, config := range []string{"ALLOWED_ORIGINS=https://old.example\n", "ALLOWED_ORIGINS=https://new.example\n"} {
		if err := os.WriteFile(envFile, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(logFile, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command("bash", "deploy.sh").CombinedOutput()
		if err != nil {
			t.Fatalf("deployment %d: %v\n%s", i, err, output)
		}
		commands, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(commands), "restart micropub-test\n") != 1 {
			t.Fatalf("deployment %d did not restart: %s", i, commands)
		}
		if i == 1 && (strings.Contains(string(commands), "daemon-reload") || !strings.Contains(string(output), "binary unchanged")) {
			t.Fatalf("expected env-only deployment: %s\n%s", commands, output)
		}
	}
}
