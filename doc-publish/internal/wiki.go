package docpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPWikiFetcher fetches one rendered page and its images from den-wiki's
// export API.
type HTTPWikiFetcher struct {
	baseURL  string
	maxBytes int64
	client   *http.Client
}

func NewHTTPWikiFetcher(cfg WikiConfig) *HTTPWikiFetcher {
	return &HTTPWikiFetcher{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		maxBytes: cfg.MaxAssetBytes,
		client:   &http.Client{Timeout: cfg.RequestTimeout},
	}
}

type wikiExportResponse struct {
	Path     string            `json:"path"`
	Title    string            `json:"title"`
	Updated  string            `json:"updated"`
	Markdown string            `json:"markdown"`
	Assets   []wikiExportAsset `json:"assets"`
	Warnings []string          `json:"warnings"`
}

type wikiExportAsset struct {
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// FetchPage fetches the page rendered with its image links under assetBase.
func (f *HTTPWikiFetcher) FetchPage(ctx context.Context, page string, assetBase string) (*SourceDocument, error) {
	query := url.Values{"page": {page}, "asset_base": {assetBase}}
	resp, err := f.get(ctx, "/v1/export?"+query.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, invalidRequest(fmt.Sprintf("wiki page fetch returned status %d", resp.StatusCode))
	}
	var export wikiExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&export); err != nil {
		return nil, fmt.Errorf("decoding wiki page: %w", err)
	}
	if export.Title == "" || export.Markdown == "" {
		return nil, invalidRequest("wiki page response requires title and markdown")
	}
	doc := &SourceDocument{
		Title:     export.Title,
		Markdown:  export.Markdown,
		UpdatedAt: parseWikiUpdated(export.Updated),
		Warnings:  export.Warnings,
	}
	for _, a := range export.Assets {
		doc.Assets = append(doc.Assets, SourceAsset(a))
	}
	return doc, nil
}

// FetchAsset fetches one listed image and checks its size and sha256.
func (f *HTTPWikiFetcher) FetchAsset(ctx context.Context, page string, asset SourceAsset) ([]byte, error) {
	if asset.Size > f.maxBytes {
		return nil, invalidRequest(fmt.Sprintf("wiki asset %s is %d bytes, over wiki.max_asset_bytes", asset.Name, asset.Size))
	}
	query := url.Values{"page": {page}, "name": {asset.Name}}
	resp, err := f.get(ctx, "/v1/export/asset?"+query.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, invalidRequest(fmt.Sprintf("wiki asset %s fetch returned status %d", asset.Name, resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading wiki asset %s: %w", asset.Name, err)
	}
	if int64(len(data)) > f.maxBytes {
		return nil, invalidRequest(fmt.Sprintf("wiki asset %s is over wiki.max_asset_bytes", asset.Name))
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != strings.ToLower(asset.SHA256) {
		return nil, invalidRequest(fmt.Sprintf("wiki asset %s does not match its sha256", asset.Name))
	}
	return data, nil
}

func (f *HTTPWikiFetcher) get(ctx context.Context, pathAndQuery string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+pathAndQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("building wiki request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching from den-wiki: %w", err)
	}
	return resp, nil
}

// parseWikiUpdated reads a page's updated field: a date or a timestamp.
// Anything else leaves the version unknown.
func parseWikiUpdated(raw string) time.Time {
	if parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), time.UTC); err == nil {
		return parsed
	}
	if parsed, err := parseCoreTimestamp(raw); err == nil {
		return parsed
	}
	return time.Time{}
}

// wikiKey names a wiki page's asset directory: wiki/ai/agent-memory.md
// becomes ai-agent-memory.
func wikiKey(page string) string {
	key := strings.TrimSuffix(strings.TrimPrefix(page, "wiki/"), ".md")
	return Slugify(strings.ReplaceAll(key, "/", "-"))
}

// safeAssetName accepts a plain file name, as den-wiki's export makes them.
func safeAssetName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
