package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type gitRepo struct {
	path string
	mu   sync.Mutex
}

func newGitRepo(path string) (*gitRepo, error) {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a git repository", path)
	}
	return &gitRepo{path: path}, nil
}

func (g *gitRepo) git(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("git %v", args)
	return cmd.Run()
}

// writeAndPush writes a file to the repo, commits, and pushes.
func (g *gitRepo) writeAndPush(filename, content, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.git("pull", "--rebase"); err != nil {
		log.Printf("warning: git pull failed (continuing): %v", err)
	}

	fullPath := filepath.Join(g.path, filename)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err := g.git("add", filename); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := g.git("commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := g.git("push"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// updateAndPush overwrites a file, commits, and pushes.
func (g *gitRepo) updateAndPush(filename, content, message string) error {
	return g.writeAndPush(filename, content, message)
}

// deleteAndPush removes a file, commits, and pushes.
func (g *gitRepo) deleteAndPush(filename, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.git("pull", "--rebase"); err != nil {
		log.Printf("warning: git pull failed (continuing): %v", err)
	}

	if err := g.git("rm", filename); err != nil {
		return fmt.Errorf("git rm: %w", err)
	}
	if err := g.git("commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := g.git("push"); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// readFile reads a file from the repo.
func (g *gitRepo) readFile(filename string) ([]byte, error) {
	return os.ReadFile(filepath.Join(g.path, filename))
}

// listFiles lists files matching a glob pattern relative to repo root.
func (g *gitRepo) listFiles(pattern string) ([]string, error) {
	return filepath.Glob(filepath.Join(g.path, pattern))
}
