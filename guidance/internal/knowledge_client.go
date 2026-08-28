package guidance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type KnowledgeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type knowledgeEntryResponse struct {
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	CurationState   string     `json:"curation_state"`
	ReplacementSlug string     `json:"replacement_slug"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastReviewedAt  *time.Time `json:"last_reviewed_at"`
	Revision        int        `json:"revision"`
}

func NewKnowledgeClient(baseURL string, token string) *KnowledgeClient {
	return &KnowledgeClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *KnowledgeClient) GetKnowledge(ctx context.Context, slug string) (*KnowledgeMetadata, error) {
	if c.baseURL == "" {
		return nil, NewServiceError(errors.New("knowledge base url is required"), "knowledge_client_unconfigured", http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/knowledge/entries/"+url.PathEscape(slug)+"/card?include_archived=true", nil)
	if err != nil {
		return nil, fmt.Errorf("building knowledge request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting knowledge entry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeTargetNotFound, slug)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("knowledge read failed: %s", errorMessage(resp))
	}
	var entry knowledgeEntryResponse
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decoding knowledge response: %w", err)
	}
	metadata := &KnowledgeMetadata{Slug: entry.Slug, Title: entry.Title, Summary: entry.Summary, Kind: entry.Kind, Status: entry.Status, CurationState: entry.CurationState, ReplacementSlug: entry.ReplacementSlug, UpdatedAt: entry.UpdatedAt, LastReviewedAt: entry.LastReviewedAt, Revision: entry.Revision, RevisionKnown: entry.Revision > 0}
	return metadata, nil
}
