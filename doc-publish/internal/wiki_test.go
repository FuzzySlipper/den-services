package docpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func sha(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func wikiSource() DocumentSource {
	return DocumentSource{WikiPage: "wiki/ai/agent-memory.md"}
}

func TestWikiSourceValidation(t *testing.T) {
	cases := map[string]PublicationRequest{
		"wiki and document": {Source: DocumentSource{WikiPage: "wiki/a.md", DocumentProjectID: "p", DocumentSlug: "s"}, RequestedBy: "pi"},
		"not under wiki":    {Source: DocumentSource{WikiPage: "sources/a.md"}, RequestedBy: "pi"},
		"escape":            {Source: DocumentSource{WikiPage: "wiki/../sources/a.md"}, RequestedBy: "pi"},
		"not markdown":      {Source: DocumentSource{WikiPage: "wiki/a.assets/x.png"}, RequestedBy: "pi"},
		"inline document":   {Source: wikiSource(), RequestedBy: "pi", Document: &SourceDocument{Title: "t", Markdown: "m"}},
	}
	for name, req := range cases {
		if err := req.Validate(true); err == nil {
			t.Errorf("%s: Validate() = nil, want error", name)
		}
	}
	if err := (PublicationRequest{Source: wikiSource(), RequestedBy: "pi"}).Validate(true); err != nil {
		t.Errorf("valid wiki source: %v", err)
	}
}

func TestHTTPWikiFetcher(t *testing.T) {
	var gotAssetBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/export":
			if r.URL.Query().Get("page") == "wiki/missing.md" {
				http.NotFound(w, r)
				return
			}
			gotAssetBase = r.URL.Query().Get("asset_base")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path": r.URL.Query().Get("page"), "title": "Agent memory", "updated": "2026-11-02",
				"markdown": "Body ![Loop](" + gotAssetBase + "loop.png)",
				"assets":   []map[string]any{{"name": "loop.png", "sha256": sha("png"), "size": 3, "content_type": "image/png"}},
				"warnings": []string{"a video is left out"},
			})
		case "/v1/export/asset":
			_, _ = w.Write([]byte("png"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	fetcher := NewHTTPWikiFetcher(WikiConfig{BaseURL: server.URL + "/", RequestTimeout: 5 * time.Second, MaxAssetBytes: 10})

	doc, err := fetcher.FetchPage(context.Background(), "wiki/agent-memory.md", "https://blog.example/assets/wiki/agent-memory/")
	if err != nil {
		t.Fatalf("FetchPage() error = %v", err)
	}
	if gotAssetBase != "https://blog.example/assets/wiki/agent-memory/" || doc.Title != "Agent memory" ||
		!doc.UpdatedAt.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) || len(doc.Assets) != 1 || len(doc.Warnings) != 1 {
		t.Fatalf("doc = %+v, asset_base %q", doc, gotAssetBase)
	}
	data, err := fetcher.FetchAsset(context.Background(), "wiki/agent-memory.md", doc.Assets[0])
	if err != nil || string(data) != "png" {
		t.Fatalf("FetchAsset() = %q, %v", data, err)
	}
	_, err = fetcher.FetchAsset(context.Background(), "wiki/agent-memory.md", SourceAsset{Name: "loop.png", SHA256: sha("other"), Size: 3})
	assertErrorContains(t, err, "does not match its sha256")
	_, err = fetcher.FetchAsset(context.Background(), "wiki/agent-memory.md", SourceAsset{Name: "loop.png", SHA256: sha("png"), Size: 11})
	assertErrorContains(t, err, "over wiki.max_asset_bytes")
	_, err = fetcher.FetchPage(context.Background(), "wiki/missing.md", "")
	assertErrorContains(t, err, "status 404")
}

type fakeWiki struct {
	document    *SourceDocument
	pageCalls   []string
	assetCalls  int
	assetBases  []string
	assetErrors error
}

func (f *fakeWiki) FetchPage(_ context.Context, page string, assetBase string) (*SourceDocument, error) {
	f.pageCalls = append(f.pageCalls, page)
	f.assetBases = append(f.assetBases, assetBase)
	return f.document, nil
}

func (f *fakeWiki) FetchAsset(_ context.Context, _ string, asset SourceAsset) ([]byte, error) {
	f.assetCalls++
	if f.assetErrors != nil {
		return nil, f.assetErrors
	}
	return []byte("bytes of " + asset.Name), nil
}

func newWikiService(t *testing.T, wiki *fakeWiki) (*Service, *fakePublisher, *FilePublicationStore) {
	t.Helper()
	publisher := &fakePublisher{commit: "abc123"}
	store := NewFilePublicationStore(t.TempDir())
	cfg := testBlogConfig(t.TempDir(), "file:///tmp/remote.git", false)
	cfg.AssetDir = "assets/wiki"
	var fetcher WikiFetcher
	if wiki != nil {
		fetcher = wiki
	}
	service, err := NewService(cfg, store, fakeFetcher{document: testDocument()}, fetcher, publisher,
		func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, publisher, store
}

func wikiDocument() *SourceDocument {
	return &SourceDocument{
		Title:    "Agent memory",
		Markdown: "Body ![Loop](https://blog.example/assets/wiki/ai-agent-memory/loop.png)",
		Assets:   []SourceAsset{{Name: "loop.png", SHA256: "x", Size: 3}, {Name: "paper.pdf", SHA256: "y", Size: 3}},
		Warnings: []string{"a video is left out"},
	}
}

func TestPreviewWikiPageListsAssetsWithoutFetchingThem(t *testing.T) {
	wiki := &fakeWiki{document: wikiDocument()}
	service, publisher, _ := newWikiService(t, wiki)
	response, err := service.Preview(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if wiki.assetBases[0] != "https://blog.example/assets/wiki/ai-agent-memory/" {
		t.Errorf("asset base = %s", wiki.assetBases[0])
	}
	if want := []string{"assets/wiki/ai-agent-memory/loop.png", "assets/wiki/ai-agent-memory/paper.pdf"}; !slices.Equal(response.AssetPaths, want) {
		t.Errorf("asset paths = %v, want %v", response.AssetPaths, want)
	}
	if !slices.Equal(response.Warnings, []string{"a video is left out"}) || response.Slug != "agent-memory" {
		t.Errorf("response = %+v", response)
	}
	if wiki.assetCalls != 0 || publisher.publishCalls != 0 {
		t.Errorf("preview fetched %d assets, published %d times", wiki.assetCalls, publisher.publishCalls)
	}
}

func TestPublishWikiPageCommitsItsAssetsAndUpdatesTheSamePost(t *testing.T) {
	wiki := &fakeWiki{document: wikiDocument()}
	service, publisher, store := newWikiService(t, wiki)
	first, err := service.Publish(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	assets := publisher.lastPost.Assets
	if len(assets) != 2 || assets[0].Path != "assets/wiki/ai-agent-memory/loop.png" || string(assets[0].Content) != "bytes of loop.png" {
		t.Fatalf("assets = %+v", assets)
	}
	record, err := store.Get(context.Background(), first.PublicationID)
	if err != nil || record.WikiPage != "wiki/ai/agent-memory.md" || len(record.AssetPaths) != 2 {
		t.Fatalf("record = %+v, %v", record, err)
	}
	second, err := service.Publish(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	if err != nil || second.PublicationID != first.PublicationID {
		t.Fatalf("second publish = %+v, %v", second, err)
	}
	// A Den document publication is never mistaken for the wiki page's.
	if _, err := store.FindBySource(context.Background(), testSource()); err == nil {
		t.Fatal("FindBySource(document) matched a wiki record")
	}
}

func TestPublishWikiPageRejectsBadAssets(t *testing.T) {
	doc := wikiDocument()
	doc.Assets = []SourceAsset{{Name: "../../_config.yml", SHA256: "x", Size: 1}}
	service, publisher, _ := newWikiService(t, &fakeWiki{document: doc})
	_, err := service.Publish(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	assertErrorContains(t, err, "not a plain file name")

	failing := &fakeWiki{document: wikiDocument(), assetErrors: invalidRequest("wiki asset loop.png does not match its sha256")}
	service, publisher, _ = newWikiService(t, failing)
	_, err = service.Publish(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	assertErrorContains(t, err, "does not match its sha256")
	if publisher.publishCalls != 0 {
		t.Fatal("a failed asset fetch must not publish")
	}
}

func TestWikiPageNeedsWikiConfigured(t *testing.T) {
	service, _, _ := newWikiService(t, nil)
	_, err := service.Preview(context.Background(), PublicationRequest{Source: wikiSource(), RequestedBy: "pi"})
	assertErrorContains(t, err, "wiki.base_url is not configured")
}

func TestGitPublisherCommitsPostAndAssetsTogether(t *testing.T) {
	repo, remote := initBlogRepo(t)
	cfg := testBlogConfig(repo, remote, true)
	cfg.AssetDir = "assets/wiki"
	publisher := NewGitPublisher(cfg, 5*time.Second)
	post := RenderedPost{
		Slug: "agent-memory", Path: "_posts/2026-11-02-agent-memory.md", Markdown: "post",
		Assets: []PostAsset{{Path: "assets/wiki/agent-memory/loop.png", Content: []byte("png")}},
	}
	if _, err := publisher.Publish(context.Background(), post, false, false); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	cmd := exec.Command("git", "show", "--name-only", "--format=", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if files := strings.Fields(string(out)); !slices.Equal(files, []string{"_posts/2026-11-02-agent-memory.md", "assets/wiki/agent-memory/loop.png"}) {
		t.Fatalf("commit files = %v", files)
	}
	if b, err := os.ReadFile(filepath.Join(repo, "assets/wiki/agent-memory/loop.png")); err != nil || string(b) != "png" {
		t.Fatalf("asset = %q, %v", b, err)
	}

	for name, assetPath := range map[string]string{
		"outside asset_dir": "_config.yml",
		"escape":            "assets/wiki/../../../escape.png",
	} {
		post := RenderedPost{Slug: "x", Path: "_posts/2026-11-02-x.md", Markdown: "x", Assets: []PostAsset{{Path: assetPath, Content: []byte("x")}}}
		if _, err := publisher.Publish(context.Background(), post, false, false); err == nil {
			t.Errorf("%s: Publish() error = nil", name)
		}
	}
	noDir := NewGitPublisher(testBlogConfig(repo, remote, false), 5*time.Second)
	_, err = noDir.Publish(context.Background(), RenderedPost{Slug: "y", Path: "_posts/2026-11-02-y.md", Markdown: "y", Assets: []PostAsset{{Path: "assets/wiki/y/a.png"}}}, false, false)
	assertErrorContains(t, err, "asset_dir is not configured")
}

func TestWikiConfigValidation(t *testing.T) {
	cfg := &Config{
		BindAddr: "127.0.0.1:8087",
		Blog:     testBlogConfig("/repo", "remote", false),
		Records:  RecordsConfig{Path: "/records"},
		Source:   SourceConfig{DocumentsBaseURL: "http://docs", RequestTimeout: time.Second},
		Git:      GitConfig{CommandTimeout: time.Second},
		HTTP:     HTTPConfig{ReadHeaderTimeout: time.Second},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("wiki disabled: validate() = %v", err)
	}
	cfg.Wiki = WikiConfig{BaseURL: "https://wiki", RequestTimeout: time.Second, MaxAssetBytes: 1}
	assertErrorContains(t, cfg.validate(), "blog.asset_dir is required")
	cfg.Blog.AssetDir = "assets/wiki"
	if err := cfg.validate(); err != nil {
		t.Fatalf("wiki enabled: validate() = %v", err)
	}
}

func TestWikiKey(t *testing.T) {
	for page, want := range map[string]string{"wiki/agent-memory.md": "agent-memory", "wiki/ai/Agent_Memory.md": "ai-agent-memory"} {
		if got := wikiKey(page); got != want {
			t.Errorf("wikiKey(%s) = %s, want %s", page, got, want)
		}
	}
}
