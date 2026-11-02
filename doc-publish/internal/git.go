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
	assetPaths := make([]string, len(post.Assets))
	for i, asset := range post.Assets {
		if assetPaths[i], err = p.safeAssetPath(asset.Path); err != nil {
			return "", err
		}
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
	for i, asset := range post.Assets {
		if err := os.MkdirAll(filepath.Dir(assetPaths[i]), 0o755); err != nil {
			return "", fmt.Errorf("creating asset directory: %w", err)
		}
		if err := os.WriteFile(assetPaths[i], asset.Content, 0o644); err != nil {
			return "", fmt.Errorf("writing asset: %w", err)
		}
	}
	add := []string{"add", "--", post.Path}
	for _, asset := range post.Assets {
		add = append(add, asset.Path)
	}
	if _, err := p.git(ctx, add...); err != nil {
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
	return p.safeRepoPath(postPath, p.cfg.PostDir, "post", "post_dir")
}

func (p *GitPublisher) safeAssetPath(assetPath string) (string, error) {
	if p.cfg.AssetDir == "" {
		return "", invalidRequest("asset path given but blog asset_dir is not configured")
	}
	return p.safeRepoPath(assetPath, p.cfg.AssetDir, "asset", "asset_dir")
}

// safeRepoPath resolves a repo-relative path that must stay inside dir.
func (p *GitPublisher) safeRepoPath(rel string, dir string, kind string, dirKey string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", invalidRequest(kind + " path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	base := filepath.Clean(filepath.FromSlash(dir))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", invalidRequest(kind + " path escapes repo")
	}
	if clean != base && !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return "", invalidRequest(kind + " path must be inside configured " + dirKey)
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
		return "", invalidRequest(kind + " path escapes repo")
	}
	return abs, nil
}

func (p *GitPublisher) git(ctx context.Context, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	cmd := exec.CommandContext(commandContext, "git", args...)
	cmd.Dir = p.cfg.RepoPath
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if commandContext.Err() != nil {
		return "", fmt.Errorf("git %s timed out: %w", strings.Join(args, " "), commandContext.Err())
	}
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}
