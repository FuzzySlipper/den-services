package docpublish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GitPublisher struct {
	cfg     BlogConfig
	timeout time.Duration
	mu      sync.Mutex
}

func NewGitPublisher(cfg BlogConfig, timeout time.Duration) *GitPublisher {
	return &GitPublisher{cfg: cfg, timeout: timeout}
}

func (p *GitPublisher) Exists(_ context.Context, postPath string) (bool, error) {
	path, err := p.safePostPath(postPath)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking post path: %w", err)
	}
	return true, nil
}

func (p *GitPublisher) Publish(ctx context.Context, post RenderedPost, overwrite bool, dryRun bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.verifyRepo(ctx); err != nil {
		return "", err
	}
	path, err := p.safePostPath(post.Path)
	if err != nil {
		return "", err
	}
	if exists, err := p.Exists(ctx, post.Path); err != nil {
		return "", err
	} else if exists && !overwrite {
		return "", invalidRequest("post path already exists and overwrite is false")
	}
	if dryRun {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating post directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(post.Markdown), 0o644); err != nil {
		return "", fmt.Errorf("writing post: %w", err)
	}
	if _, err := p.git(ctx, "add", "--", post.Path); err != nil {
		return "", err
	}
	message := "Publish " + post.Slug
	if _, err := p.git(ctx, "-c", "user.name="+p.cfg.AuthorName, "-c", "user.email="+p.cfg.AuthorEmail, "commit", "-m", message); err != nil {
		return "", err
	}
	commit, err := p.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if p.cfg.Push {
		if _, err := p.git(ctx, "push", "origin", p.cfg.Branch); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(commit), nil
}

func (p *GitPublisher) verifyRepo(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(p.cfg.RepoPath, ".git")); err != nil {
		return repoUnsafe("repo_path is not a git repo")
	}
	branch, err := p.git(ctx, "branch", "--show-current")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != p.cfg.Branch {
		return repoUnsafe(fmt.Sprintf("wrong branch %s", strings.TrimSpace(branch)))
	}
	remote, err := p.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if strings.TrimSpace(remote) != p.cfg.ExpectedRemoteURL {
		return repoUnsafe("origin remote does not match expected_remote_url")
	}
	status, err := p.git(ctx, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return repoUnsafe("repo has uncommitted changes")
	}
	return nil
}

func (p *GitPublisher) safePostPath(postPath string) (string, error) {
	if postPath == "" || filepath.IsAbs(postPath) {
		return "", invalidRequest("post path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(postPath))
	postDir := filepath.Clean(filepath.FromSlash(p.cfg.PostDir))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", invalidRequest("post path escapes repo")
	}
	if clean != postDir && !strings.HasPrefix(clean, postDir+string(filepath.Separator)) {
		return "", invalidRequest("post path must be inside configured post_dir")
	}
	full := filepath.Join(p.cfg.RepoPath, clean)
	root, err := filepath.Abs(p.cfg.RepoPath)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", invalidRequest("post path escapes repo")
	}
	return abs, nil
}

func (p *GitPublisher) git(ctx context.Context, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", args...)
	cmd.Dir = p.cfg.RepoPath
	output, err := cmd.CombinedOutput()
	if commandContext.Err() != nil {
		return "", fmt.Errorf("git %s timed out: %w", strings.Join(args, " "), commandContext.Err())
	}
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
