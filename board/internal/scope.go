package board

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

func (NoopProjectValidator) AssertWritable(context.Context, string) error {
	return nil
}

type ProjectScopeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewProjectScopeClient(baseURL string, token string, timeout time.Duration) *ProjectScopeClient {
	return &ProjectScopeClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *ProjectScopeClient) AssertWritable(ctx context.Context, projectID string) error {
	if c == nil || c.baseURL == "" {
		return NewServiceError(ErrProjectValidatorUnset, "project_scope_client_unconfigured", http.StatusInternalServerError)
	}
	endpoint := c.baseURL + "/v1/scopes/" + url.PathEscape(projectID) + "/assert-writable"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(`{}`))
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
	message := projectErrorMessage(resp)
	if resp.StatusCode == http.StatusConflict {
		return conflict(fmt.Errorf("project scope is not writable: %s", message), "project_scope_not_writable")
	}
	if resp.StatusCode == http.StatusNotFound {
		return validationFailed(fmt.Errorf("project scope not found: %s", projectID))
	}
	return fmt.Errorf("project scope writable check failed: %s", message)
}

func projectErrorMessage(resp *http.Response) string {
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
