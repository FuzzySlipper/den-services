package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrProjectScopeClientUnconfigured = errors.New("projects scope client is not configured") //nolint:gochecknoglobals

type NoopScopeValidator struct{}

func (NoopScopeValidator) AssertWritable(context.Context, string) error {
	return nil
}

type ProjectScopeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewProjectScopeClient(baseURL string, token string) *ProjectScopeClient {
	return &ProjectScopeClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *ProjectScopeClient) AssertWritable(ctx context.Context, projectID string) error {
	if c.baseURL == "" {
		return NewServiceError(ErrProjectScopeClientUnconfigured, "project_scope_client_unconfigured", http.StatusInternalServerError)
	}
	body := bytes.NewBufferString(`{}`)
	escapedProjectID := url.PathEscape(projectID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/scopes/"+escapedProjectID+"/assert-writable", body)
	if err != nil {
		return fmt.Errorf("building project assert-writable request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("asserting project scope writable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = response.Status
	}
	if response.StatusCode == http.StatusConflict {
		return conflict(fmt.Errorf("project scope is not writable: %s", message), "project_scope_not_writable")
	}
	if response.StatusCode == http.StatusNotFound {
		return validationFailed(fmt.Errorf("project scope not found: %s", projectID))
	}
	return fmt.Errorf("project scope writable check failed: %s", message)
}
