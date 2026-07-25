package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NoopProjectValidator struct{}

func (NoopProjectValidator) AssertWritable(context.Context, string) error { return nil }

type StaticGuidanceReader struct {
	References []DocumentReference
	Ready      bool
}

func (r StaticGuidanceReader) DocumentReferences(context.Context, string, string) ([]DocumentReference, bool, error) {
	return append([]DocumentReference(nil), r.References...), r.Ready, nil
}

type ProjectScopeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewProjectScopeClient(baseURL string, token string) *ProjectScopeClient {
	return &ProjectScopeClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *ProjectScopeClient) AssertWritable(ctx context.Context, projectID string) error {
	if c.baseURL == "" {
		return NewServiceError(ErrProjectClientUnset, "project_scope_client_unconfigured", http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/scopes/"+url.PathEscape(projectID)+"/assert-writable", bytes.NewBufferString(`{}`))
	if err != nil {
		return fmt.Errorf("building project assert-writable request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("asserting project scope writable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	message := errorMessage(resp)
	if resp.StatusCode == http.StatusConflict {
		return conflict(fmt.Errorf("project scope is not writable: %s", message), "project_scope_not_writable")
	}
	if resp.StatusCode == http.StatusNotFound {
		return validationFailed(fmt.Errorf("project scope not found: %s", projectID))
	}
	return fmt.Errorf("project scope writable check failed: %s", message)
}

type AgentGuidanceClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAgentGuidanceClient(baseURL string, token string) *AgentGuidanceClient {
	return &AgentGuidanceClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *AgentGuidanceClient) DocumentReferences(ctx context.Context, projectID string, slug string) ([]DocumentReference, bool, error) {
	if c.baseURL == "" {
		return nil, false, nil
	}
	endpoint := fmt.Sprintf("%s/v1/guidance/document-references?document_project_id=%s&document_slug=%s", c.baseURL, url.QueryEscape(projectID), url.QueryEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("building guidance reference request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("checking guidance references: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("guidance reference check failed: %s", errorMessage(resp))
	}
	var envelope struct {
		References   []DocumentReferenceResponse `json:"references"`
		ReferencedBy []DocumentReferenceResponse `json:"referenced_by"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, false, fmt.Errorf("decoding guidance references: %w", err)
	}
	source := envelope.References
	if len(source) == 0 {
		source = envelope.ReferencedBy
	}
	refs := make([]DocumentReference, 0, len(source))
	for _, ref := range source {
		refs = append(refs, DocumentReference(ref))
	}
	return refs, true, nil
}

func errorMessage(resp *http.Response) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&envelope)
	if strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return resp.Status
}
