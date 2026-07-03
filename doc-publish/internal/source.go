package docpublish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPDocumentFetcher struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPDocumentFetcher(baseURL string, token string, timeout time.Duration) *HTTPDocumentFetcher {
	return &HTTPDocumentFetcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: timeout},
	}
}

func (f *HTTPDocumentFetcher) Fetch(ctx context.Context, source DocumentSource) (*SourceDocument, error) {
	if source.DocumentProjectID == "" || source.DocumentSlug == "" {
		return nil, invalidRequest("source document project and slug are required")
	}
	endpoints := documentSourceEndpoints(f.baseURL, source)
	var lastStatus int
	for _, endpoint := range endpoints {
		doc, status, err := f.fetchEndpoint(ctx, endpoint, source)
		if err != nil {
			return nil, err
		}
		if status == http.StatusOK {
			return doc, nil
		}
		lastStatus = status
		if status != http.StatusNotFound {
			break
		}
	}
	return nil, invalidRequest(fmt.Sprintf("source document fetch returned status %d", lastStatus))
}

func (f *HTTPDocumentFetcher) fetchEndpoint(ctx context.Context, endpoint string, source DocumentSource) (*SourceDocument, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("building source document request: %w", err)
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetching source document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var coreDoc coreDocumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&coreDoc); err != nil {
		return nil, 0, fmt.Errorf("decoding source document: %w", err)
	}
	doc := SourceDocument{
		Title:     firstNonEmpty(coreDoc.Title, coreDoc.Slug, source.DocumentSlug),
		Slug:      coreDoc.Slug,
		Markdown:  coreDoc.Content,
		UpdatedAt: coreDoc.UpdatedAt.Time,
	}
	if doc.Title == "" || doc.Markdown == "" {
		return nil, 0, invalidRequest("source document response requires title and content")
	}
	return &doc, http.StatusOK, nil
}

func documentSourceEndpoints(baseURL string, source DocumentSource) []string {
	escapedProject := url.PathEscape(source.DocumentProjectID)
	escapedSlug := url.PathEscape(source.DocumentSlug)
	documentPath := "/projects/" + escapedProject + "/documents/" + escapedSlug

	normalized := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(normalized, "/api") || strings.HasSuffix(normalized, "/v1") || strings.HasSuffix(normalized, "/api/v1") {
		return []string{normalized + documentPath}
	}
	return []string{
		normalized + "/api" + documentPath,
		normalized + "/v1" + documentPath,
		normalized + "/api/v1" + documentPath,
	}
}

type coreDocumentResponse struct {
	Title     string        `json:"title"`
	Slug      string        `json:"slug"`
	Content   string        `json:"content"`
	UpdatedAt coreTimestamp `json:"updated_at"`
}

type coreTimestamp struct {
	time.Time
}

func (t *coreTimestamp) UnmarshalJSON(data []byte) error {
	var raw *string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := parseCoreTimestamp(*raw)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func parseCoreTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.UTC); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", value, time.UTC); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("parsing Core document timestamp %q as RFC3339 or UTC local timestamp", raw)
}
