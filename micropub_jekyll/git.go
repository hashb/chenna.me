package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (g *gitRepo) gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.path
	log.Printf("git %v", args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmedOutput)
	}
	return strings.TrimSpace(string(output)), nil
}

func (g *gitRepo) currentBranch() (string, error) {
	branch, err := g.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("repository is not on a branch")
	}
	return branch, nil
}

func (g *gitRepo) syncToRemote() error {
	branch, err := g.currentBranch()
	if err != nil {
		return err
	}
	if err := g.git("fetch", "--prune", "origin"); err != nil {
		return fmt.Errorf("git fetch --prune origin: %w", err)
	}
	if err := g.git("reset", "--hard", "origin/"+branch); err != nil {
		return fmt.Errorf("git reset --hard origin/%s: %w", branch, err)
	}
	return nil
}

func (g *gitRepo) rollbackToRemote(pathspec string) error {
	rollbackErr := g.syncToRemote()
	if pathspec == "" {
		return rollbackErr
	}
	cleanErr := g.git("clean", "-fd", "--", pathspec)
	return errors.Join(rollbackErr, cleanErr)
}

func wrapWithRollback(primaryErr, rollbackErr error) error {
	if rollbackErr == nil {
		return primaryErr
	}
	return errors.Join(primaryErr, fmt.Errorf("rollback to remote: %w", rollbackErr))
}

// writeAndPush writes a file to the repo, commits, and pushes.
func (g *gitRepo) writeAndPush(filename, content, message string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.syncToRemote(); err != nil {
		return fmt.Errorf("sync repo: %w", err)
	}

	fullPath := filepath.Join(g.path, filename)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return wrapWithRollback(fmt.Errorf("write file: %w", err), g.rollbackToRemote(filename))
	}

	if err := g.git("add", filename); err != nil {
		return wrapWithRollback(fmt.Errorf("git add: %w", err), g.rollbackToRemote(filename))
	}
	if err := g.git("commit", "-m", message); err != nil {
		return wrapWithRollback(fmt.Errorf("git commit: %w", err), g.rollbackToRemote(filename))
	}
	if err := g.git("push"); err != nil {
		return wrapWithRollback(fmt.Errorf("git push: %w", err), g.rollbackToRemote(filename))
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

	if err := g.syncToRemote(); err != nil {
		return fmt.Errorf("sync repo: %w", err)
	}

	if err := g.git("rm", filename); err != nil {
		return wrapWithRollback(fmt.Errorf("git rm: %w", err), g.rollbackToRemote(filename))
	}
	if err := g.git("commit", "-m", message); err != nil {
		return wrapWithRollback(fmt.Errorf("git commit: %w", err), g.rollbackToRemote(filename))
	}
	if err := g.git("push"); err != nil {
		return wrapWithRollback(fmt.Errorf("git push: %w", err), g.rollbackToRemote(filename))
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
