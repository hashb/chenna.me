package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncToRemoteRejectsRepoAheadOfUpstream(t *testing.T) {
	repoPaths := newTempGitRepo(t)

	localOnlyPath := filepath.Join(repoPaths.localDir, "local-only.txt")
	if err := os.WriteFile(localOnlyPath, []byte("local only\n"), 0o644); err != nil {
		t.Fatalf("write local-only file: %v", err)
	}
	runGit(t, repoPaths.localDir, "add", "local-only.txt")
	runGit(t, repoPaths.localDir, "commit", "-m", "local-only")

	repo, err := newGitRepo(repoPaths.localDir)
	if err != nil {
		t.Fatalf("newGitRepo: %v", err)
	}

	err = repo.syncToRemote()
	if err == nil {
		t.Fatal("syncToRemote succeeded even though the repository was ahead of upstream")
	}
	if !strings.Contains(err.Error(), "ahead of upstream") {
		t.Fatalf("syncToRemote error = %v, want ahead-of-upstream failure", err)
	}

	if _, err := os.Stat(localOnlyPath); err != nil {
		t.Fatalf("local-only file missing after rejected syncToRemote: %v", err)
	}
	if got := runGit(t, repoPaths.localDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want empty", got)
	}
	if got := runGit(t, repoPaths.localDir, "rev-list", "--left-right", "--count", "HEAD...origin/main"); got != "1\t0" {
		t.Fatalf("git rev-list --left-right --count HEAD...origin/main = %q, want %q", got, "1\t0")
	}
}

func TestSyncToRemoteFastForwardsRemoteChanges(t *testing.T) {
	repoPaths := newTempGitRepo(t)

	peerDir := filepath.Join(t.TempDir(), "peer")
	runGit(t, filepath.Dir(peerDir), "clone", repoPaths.remoteDir, peerDir)

	seedPath := filepath.Join(peerDir, "README.md")
	if err := os.WriteFile(seedPath, []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("write peer update: %v", err)
	}
	runGit(t, peerDir, "add", "README.md")
	runGit(t, peerDir, "commit", "-m", "update remote")
	runGit(t, peerDir, "push", "origin", "main")

	repo, err := newGitRepo(repoPaths.localDir)
	if err != nil {
		t.Fatalf("newGitRepo: %v", err)
	}

	if err := repo.syncToRemote(); err != nil {
		t.Fatalf("syncToRemote: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(repoPaths.localDir, "README.md"))
	if err != nil {
		t.Fatalf("read local README: %v", err)
	}
	if string(content) != "updated\n" {
		t.Fatalf("README.md = %q, want %q", string(content), "updated\n")
	}
	if got := runGit(t, repoPaths.localDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want empty", got)
	}
	head := runGit(t, repoPaths.localDir, "rev-parse", "HEAD")
	originHead := runGit(t, repoPaths.localDir, "rev-parse", "origin/main")
	if head != originHead {
		t.Fatalf("HEAD = %q, want %q", head, originHead)
	}
}

func TestWriteAndPushRollsBackToPreviousHeadOnPushFailure(t *testing.T) {
	repoPaths := newTempGitRepo(t)

	hookPath := filepath.Join(repoPaths.remoteDir, "hooks", "pre-receive")
	hookContents := "#!/bin/sh\necho 'rejecting push for test' >&2\nexit 1\n"
	if err := os.WriteFile(hookPath, []byte(hookContents), 0o755); err != nil {
		t.Fatalf("write pre-receive hook: %v", err)
	}

	repo, err := newGitRepo(repoPaths.localDir)
	if err != nil {
		t.Fatalf("newGitRepo: %v", err)
	}
	startingHead := runGit(t, repoPaths.localDir, "rev-parse", "HEAD")

	filename := "_micros/2026/2026-04-03-143000.md"
	err = repo.writeAndPush(filename, "hello\n", "micropub: create micro-post test")
	if err == nil {
		t.Fatal("writeAndPush succeeded even though the remote rejected the push")
	}

	fullPath := filepath.Join(repoPaths.localDir, filename)
	if _, statErr := os.Stat(fullPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("post file still exists after rollback: %v", statErr)
	}
	if got := runGit(t, repoPaths.localDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status --porcelain = %q, want empty", got)
	}
	head := runGit(t, repoPaths.localDir, "rev-parse", "HEAD")
	if head != startingHead {
		t.Fatalf("HEAD = %q, want %q", head, startingHead)
	}
}

type tempGitRepoPaths struct {
	remoteDir string
	localDir  string
}

func newTempGitRepo(t *testing.T) tempGitRepoPaths {
	t.Helper()

	rootDir := t.TempDir()
	remoteDir := filepath.Join(rootDir, "remote.git")
	runGit(t, rootDir, "init", "--bare", "--initial-branch=main", remoteDir)

	localDir := filepath.Join(rootDir, "local")
	runGit(t, rootDir, "clone", remoteDir, localDir)

	seedPath := filepath.Join(localDir, "README.md")
	if err := os.WriteFile(seedPath, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runGit(t, localDir, "add", "README.md")
	runGit(t, localDir, "commit", "-m", "seed")
	runGit(t, localDir, "push", "-u", "origin", "main")

	return tempGitRepoPaths{remoteDir: remoteDir, localDir: localDir}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Micropub Jekyll Tests",
		"GIT_AUTHOR_EMAIL=tests@example.com",
		"GIT_COMMITTER_NAME=Micropub Jekyll Tests",
		"GIT_COMMITTER_EMAIL=tests@example.com",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}
