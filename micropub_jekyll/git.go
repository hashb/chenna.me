package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func (g *gitRepo) currentHead() (string, error) {
	head, err := g.gitOutput("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	if head == "" {
		return "", fmt.Errorf("repository has no HEAD commit")
	}
	return head, nil
}

func (g *gitRepo) ensureCleanWorktree() error {
	status, err := g.gitOutput("status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("git status --porcelain --untracked-files=all: %w", err)
	}
	if status != "" {
		return fmt.Errorf("repository has local changes")
	}
	return nil
}

func (g *gitRepo) ensureNotAheadOfUpstream() error {
	counts, err := g.gitOutput("rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return fmt.Errorf("git rev-list --left-right --count HEAD...@{upstream}: %w", err)
	}

	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return fmt.Errorf("unexpected upstream distance output %q", counts)
	}

	ahead, err := strconv.Atoi(fields[0])
	if err != nil {
		return fmt.Errorf("parse ahead commit count %q: %w", fields[0], err)
	}
	if ahead > 0 {
		return fmt.Errorf("repository is ahead of upstream by %d commit(s)", ahead)
	}

	return nil
}

func (g *gitRepo) syncToRemote() error {
	if err := g.ensureCleanWorktree(); err != nil {
		return err
	}
	if err := g.ensureNotAheadOfUpstream(); err != nil {
		return err
	}
	if err := g.git("pull", "--ff-only", "--no-rebase"); err != nil {
		return fmt.Errorf("git pull --ff-only --no-rebase: %w", err)
	}
	return nil
}

func (g *gitRepo) rollbackToHead(head, pathspec string) error {
	resetErr := g.git("reset", "--hard", head)
	if pathspec == "" {
		return resetErr
	}
	cleanErr := g.git("clean", "-fd", "--", pathspec)
	return errors.Join(resetErr, cleanErr)
}

func wrapWithRollback(primaryErr, rollbackErr error) error {
	if rollbackErr == nil {
		return primaryErr
	}
	return errors.Join(primaryErr, fmt.Errorf("rollback to remote: %w", rollbackErr))
}

// writeAndPush writes a file to the repo, commits, and pushes.
func (g *gitRepo) writeAndPush(filename, content, message string) error {
	return g.mutateAndPush(filename, message, func(fullPath string) error {
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		return nil
	}, func() error {
		if err := g.git("add", "--", filename); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		return nil
	})
}

func (g *gitRepo) mutateAndPush(pathspec, message string, mutate func(fullPath string) error, stage func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.syncToRemote(); err != nil {
		return fmt.Errorf("sync repo: %w", err)
	}

	rollbackHead, err := g.currentHead()
	if err != nil {
		return err
	}

	fullPath := filepath.Join(g.path, pathspec)
	if err := mutate(fullPath); err != nil {
		return wrapWithRollback(err, g.rollbackToHead(rollbackHead, pathspec))
	}

	if err := stage(); err != nil {
		return wrapWithRollback(err, g.rollbackToHead(rollbackHead, pathspec))
	}
	if err := g.git("commit", "-m", message); err != nil {
		return wrapWithRollback(fmt.Errorf("git commit: %w", err), g.rollbackToHead(rollbackHead, pathspec))
	}
	if err := g.git("push"); err != nil {
		return wrapWithRollback(fmt.Errorf("git push: %w", err), g.rollbackToHead(rollbackHead, pathspec))
	}
	return nil
}

// updateAndPush overwrites a file, commits, and pushes.
func (g *gitRepo) updateAndPush(filename, content, message string) error {
	return g.writeAndPush(filename, content, message)
}

// deleteAndPush removes a file, commits, and pushes.
func (g *gitRepo) deleteAndPush(filename, message string) error {
	return g.mutateAndPush(filename, message, func(string) error {
		return nil
	}, func() error {
		if err := g.git("rm", "--", filename); err != nil {
			return fmt.Errorf("git rm: %w", err)
		}
		return nil
	})
}

// readFile reads a file from the repo.
func (g *gitRepo) readFile(filename string) ([]byte, error) {
	return os.ReadFile(filepath.Join(g.path, filename))
}

// listFiles lists files matching a glob pattern relative to repo root.
func (g *gitRepo) listFiles(pattern string) ([]string, error) {
	return filepath.Glob(filepath.Join(g.path, pattern))
}
