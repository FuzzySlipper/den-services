package docpublish

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugifyAndRenderPost(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	post, err := RenderPost(&SourceDocument{
		Title: "Hello, Den!",
		Markdown: `---
title: old
---
# Body
`,
	}, PublicationOptions{Tags: []string{"Den Docs", "Go!"}}, "_posts", "https://blog.example", now)
	if err != nil {
		t.Fatalf("RenderPost() error = %v", err)
	}
	if post.Slug != "hello-den" {
		t.Fatalf("slug = %s, want hello-den", post.Slug)
	}
	if post.Path != "_posts/2026-06-22-hello-den.md" {
		t.Fatalf("path = %s", post.Path)
	}
	assertContains(t, post.Markdown, `layout: post`)
	assertContains(t, post.Markdown, `title: "Hello, Den!"`)
	assertContains(t, post.Markdown, `tags: ["den-docs", "go"]`)
	assertContains(t, post.Markdown, "# Body")
	if strings.Contains(post.Markdown, "title: old") {
		t.Fatal("source frontmatter should be stripped")
	}
}

func TestPreviewDoesNotWrite(t *testing.T) {
	service, publisher := newTestService(t)
	response, err := service.Preview(context.Background(), PublicationRequest{
		Source:      testSource(),
		RequestedBy: "pi",
		Document:    testDocument(),
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if response.Status != PublicationStatusPreviewed || !response.DryRun {
		t.Fatalf("preview status/dry_run = %s/%v", response.Status, response.DryRun)
	}
	if publisher.publishCalls != 0 {
		t.Fatalf("publish calls = %d, want 0", publisher.publishCalls)
	}
}

func TestPublishFetchesCanonicalDocumentAndStoresRecord(t *testing.T) {
	service, publisher := newTestService(t)
	response, err := service.Publish(context.Background(), PublicationRequest{
		Source:      testSource(),
		RequestedBy: "pi",
		Document:    &SourceDocument{Title: "Browser text", Markdown: "should not publish"},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if response.Status != PublicationStatusPublished {
		t.Fatalf("status = %s", response.Status)
	}
	if !strings.Contains(publisher.lastPost.Markdown, "Canonical body") {
		t.Fatal("publish should use fetched canonical document, not request payload")
	}
	record, err := service.Get(context.Background(), response.PublicationID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if record.GitCommit != "abc123" || record.Status != PublicationStatusPublished {
		t.Fatalf("record = %+v", record)
	}
}

func TestPublishUpdatesExistingPublicationWithoutOverwrite(t *testing.T) {
	service, publisher := newTestService(t)
	first, err := service.Publish(context.Background(), PublicationRequest{
		Source:      testSource(),
		RequestedBy: "pi",
	})
	if err != nil {
		t.Fatalf("first Publish() error = %v", err)
	}
	second, err := service.Publish(context.Background(), PublicationRequest{
		Source:      testSource(),
		RequestedBy: "pi",
	})
	if err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
	if second.PublicationID != first.PublicationID {
		t.Fatalf("publication id changed: %s -> %s", first.PublicationID, second.PublicationID)
	}
	if publisher.publishCalls != 2 {
		t.Fatalf("publish calls = %d, want 2", publisher.publishCalls)
	}
}

func TestGitPublisherPublishesToTempRepo(t *testing.T) {
	repo, remote := initBlogRepo(t)
	cfg := testBlogConfig(repo, remote, true)
	publisher := NewGitPublisher(cfg, 5*time.Second)
	post := RenderedPost{Slug: "hello", Path: "_posts/2026-06-22-hello.md", Markdown: "---\nlayout: post\n---\nhello"}
	commit, err := publisher.Publish(context.Background(), post, false, false)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if commit == "" {
		t.Fatal("commit is empty")
	}
	if _, err := os.Stat(filepath.Join(repo, post.Path)); err != nil {
		t.Fatalf("published file missing: %v", err)
	}
}

func TestGitPublisherSafetyChecks(t *testing.T) {
	t.Run("duplicate without overwrite", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		cfg := testBlogConfig(repo, remote, false)
		publisher := NewGitPublisher(cfg, 5*time.Second)
		path := filepath.Join(repo, "_posts/2026-06-22-existing.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", "_posts/2026-06-22-existing.md")
		runGit(t, repo, "commit", "-m", "existing post")
		_, err := publisher.Publish(context.Background(), RenderedPost{Slug: "existing", Path: "_posts/2026-06-22-existing.md", Markdown: "new"}, false, false)
		assertErrorContains(t, err, "post path already exists")
	})
	t.Run("dirty repo", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
			t.Fatal(err)
		}
		publisher := NewGitPublisher(testBlogConfig(repo, remote, false), 5*time.Second)
		_, err := publisher.Publish(context.Background(), RenderedPost{Slug: "x", Path: "_posts/2026-06-22-x.md", Markdown: "x"}, false, false)
		assertErrorContains(t, err, "repo has uncommitted changes")
	})
	t.Run("wrong branch", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		cfg := testBlogConfig(repo, remote, false)
		cfg.Branch = "other"
		publisher := NewGitPublisher(cfg, 5*time.Second)
		_, err := publisher.Publish(context.Background(), RenderedPost{Slug: "x", Path: "_posts/2026-06-22-x.md", Markdown: "x"}, false, false)
		assertErrorContains(t, err, "wrong branch")
	})
	t.Run("wrong remote", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		cfg := testBlogConfig(repo, remote+"-wrong", false)
		publisher := NewGitPublisher(cfg, 5*time.Second)
		_, err := publisher.Publish(context.Background(), RenderedPost{Slug: "x", Path: "_posts/2026-06-22-x.md", Markdown: "x"}, false, false)
		assertErrorContains(t, err, "origin remote does not match")
	})
	t.Run("path traversal", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		publisher := NewGitPublisher(testBlogConfig(repo, remote, false), 5*time.Second)
		_, err := publisher.Publish(context.Background(), RenderedPost{Slug: "x", Path: "../escape.md", Markdown: "x"}, false, false)
		assertErrorContains(t, err, "post path escapes repo")
	})
	t.Run("dry run does not write", func(t *testing.T) {
		repo, remote := initBlogRepo(t)
		publisher := NewGitPublisher(testBlogConfig(repo, remote, false), 5*time.Second)
		post := RenderedPost{Slug: "dry", Path: "_posts/2026-06-22-dry.md", Markdown: "dry"}
		if _, err := publisher.Publish(context.Background(), post, false, true); err != nil {
			t.Fatalf("Publish(dryRun) error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(repo, post.Path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry run file stat error = %v, want not exist", err)
		}
	})
}

func TestConfigValidation(t *testing.T) {
	cfg := &Config{}
	if err := cfg.validate(); err == nil {
		t.Fatal("validate() error = nil, want error")
	}
}

func newTestService(t *testing.T) (*Service, *fakePublisher) {
	t.Helper()
	publisher := &fakePublisher{commit: "abc123"}
	service, err := NewService(
		testBlogConfig(t.TempDir(), "file:///tmp/remote.git", false),
		NewFilePublicationStore(t.TempDir()),
		fakeFetcher{document: testDocument()},
		publisher,
		func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, publisher
}

type fakeFetcher struct {
	document *SourceDocument
}

func (f fakeFetcher) Fetch(_ context.Context, _ DocumentSource) (*SourceDocument, error) {
	return f.document, nil
}

type fakePublisher struct {
	commit       string
	publishCalls int
	lastPost     RenderedPost
}

func (p *fakePublisher) Publish(_ context.Context, post RenderedPost, _ bool, _ bool) (string, error) {
	p.publishCalls++
	p.lastPost = post
	return p.commit, nil
}

func (p *fakePublisher) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func testSource() DocumentSource {
	return DocumentSource{ProjectID: "den-web", DocumentProjectID: "den-web", DocumentSlug: "example-doc"}
}

func testDocument() *SourceDocument {
	return &SourceDocument{Title: "Canonical Title", Markdown: "Canonical body"}
}

func testBlogConfig(repo string, remote string, push bool) BlogConfig {
	return BlogConfig{
		RepoPath:          repo,
		ExpectedRemoteURL: remote,
		Branch:            "main",
		PostDir:           "_posts",
		PublicBaseURL:     "https://blog.example",
		AuthorName:        "Den Publisher",
		AuthorEmail:       "den@example.invalid",
		Push:              push,
	}
}

func initBlogRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("blog"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "push", "-u", "origin", "main")
	return repo, remote
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func assertContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("text does not contain %q\n%s", want, text)
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

func TestErrorsWrapSentinels(t *testing.T) {
	if !errors.Is(invalidRequest("x"), ErrInvalidRequest) {
		t.Fatal("invalidRequest should wrap ErrInvalidRequest")
	}
	if !errors.Is(repoUnsafe("x"), ErrRepoUnsafe) {
		t.Fatal("repoUnsafe should wrap ErrRepoUnsafe")
	}
}
