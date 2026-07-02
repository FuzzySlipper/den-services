package guidance

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
		return NewServiceError(fmt.Errorf("projects base url is required"), "project_scope_client_unconfigured", http.StatusInternalServerError)
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
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return validationFailed(fmt.Errorf("project scope not found: %s", projectID))
	}
	if resp.StatusCode == http.StatusConflict {
		return NewServiceError(fmt.Errorf("project scope is not writable: %s", errorMessage(resp)), "project_scope_not_writable", http.StatusConflict)
	}
	return fmt.Errorf("project scope writable check failed: %s", errorMessage(resp))
}

type DocumentsClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type documentResponse struct {
	ProjectID  string    `json:"project_id"`
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	DocType    string    `json:"doc_type"`
	Visibility string    `json:"visibility"`
	Tags       []string  `json:"tags,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func NewDocumentsClient(baseURL string, token string) *DocumentsClient {
	return &DocumentsClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *DocumentsClient) GetDocument(ctx context.Context, projectID string, slug string) (*Document, error) {
	if c.baseURL == "" {
		return nil, NewServiceError(fmt.Errorf("documents base url is required"), "documents_client_unconfigured", http.StatusInternalServerError)
	}
	endpoint := c.baseURL + "/v1/projects/" + url.PathEscape(projectID) + "/documents/" + url.PathEscape(slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building document request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, NewServiceError(fmt.Errorf("%w: %s/%s", ErrDocumentUnavailable, projectID, slug), "document_not_found", http.StatusNotFound)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("document read failed: %s", errorMessage(resp))
	}
	var response documentResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decoding document response: %w", err)
	}
	return &Document{
		ProjectID:  response.ProjectID,
		Slug:       response.Slug,
		Title:      response.Title,
		Content:    response.Content,
		DocType:    response.DocType,
		Visibility: response.Visibility,
		Tags:       append([]string(nil), response.Tags...),
		Summary:    response.Summary,
		UpdatedAt:  response.UpdatedAt,
	}, nil
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
