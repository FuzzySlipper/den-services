package librarian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SourceClientConfig struct {
	ProjectsBaseURL  string
	ProjectsToken    string
	TasksBaseURL     string
	TasksToken       string
	MessagesBaseURL  string
	MessagesToken    string
	DocumentsBaseURL string
	DocumentsToken   string
	KnowledgeBaseURL string
	KnowledgeToken   string
	RequestTimeout   time.Duration
}

type HTTPSourceClients struct {
	projects  sourceClient
	tasks     sourceClient
	messages  sourceClient
	documents sourceClient
	knowledge sourceClient
}

type sourceClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPSourceClients(cfg SourceClientConfig) *HTTPSourceClients {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	return &HTTPSourceClients{
		projects:  newSourceClient(cfg.ProjectsBaseURL, cfg.ProjectsToken, client),
		tasks:     newSourceClient(cfg.TasksBaseURL, cfg.TasksToken, client),
		messages:  newSourceClient(cfg.MessagesBaseURL, cfg.MessagesToken, client),
		documents: newSourceClient(cfg.DocumentsBaseURL, cfg.DocumentsToken, client),
		knowledge: newSourceClient(cfg.KnowledgeBaseURL, cfg.KnowledgeToken, client),
	}
}

func newSourceClient(baseURL string, token string, client *http.Client) sourceClient {
	return sourceClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: client}
}

func (c *HTTPSourceClients) ValidateProject(ctx context.Context, projectID string) error {
	endpoint := c.projects.baseURL + "/v1/scopes/" + url.PathEscape(projectID)
	var scope struct {
		ID string `json:"id"`
	}
	if err := c.projects.getJSON(ctx, endpoint, &scope); err != nil {
		if errors.Is(err, ErrSourceNotFound) {
			return notFound(fmt.Errorf("project scope not found: %s", projectID))
		}
		return err
	}
	return nil
}

func (c *HTTPSourceClients) GetTask(ctx context.Context, projectID string, taskID int64) (TaskDetail, error) {
	endpoint := c.tasks.baseURL + "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(taskID, 10)
	var detail TaskDetail
	if err := c.tasks.getJSON(ctx, endpoint, &detail); err != nil {
		if errors.Is(err, ErrSourceNotFound) {
			return TaskDetail{}, notFound(fmt.Errorf("%w: %d", ErrTaskNotFound, taskID))
		}
		return TaskDetail{}, err
	}
	if detail.Task.ProjectID != projectID {
		return TaskDetail{}, validationFailed(fmt.Errorf("%w: task %d belongs to %s", ErrProjectMismatch, taskID, detail.Task.ProjectID))
	}
	return detail, nil
}

func (c *HTTPSourceClients) ListTasks(ctx context.Context, projectID string, limit int) ([]TaskSummary, error) {
	endpoint, err := url.Parse(c.tasks.baseURL + "/v1/projects/" + url.PathEscape(projectID) + "/tasks")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("include_all", "true")
	endpoint.RawQuery = query.Encode()
	var tasks []TaskSummary
	if err := c.tasks.getJSON(ctx, endpoint.String(), &tasks); err != nil {
		return nil, err
	}
	if len(tasks) > limit {
		return tasks[:limit], nil
	}
	return tasks, nil
}

func (c *HTTPSourceClients) ListMessages(ctx context.Context, projectID string, taskID *int64, limit int) ([]MessageSummary, error) {
	endpoint, err := url.Parse(c.messages.baseURL + "/v1/projects/" + url.PathEscape(projectID) + "/messages")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("limit", strconv.Itoa(limit))
	if taskID != nil {
		query.Set("task_id", strconv.FormatInt(*taskID, 10))
	}
	endpoint.RawQuery = query.Encode()
	var messages []MessageSummary
	if err := c.messages.getJSON(ctx, endpoint.String(), &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (c *HTTPSourceClients) SearchDocuments(ctx context.Context, projectID string, queryText string, limit int) ([]DocumentSearchResult, error) {
	endpoint, err := url.Parse(c.documents.baseURL + "/v1/projects/" + url.PathEscape(projectID) + "/documents/search")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("query", queryText)
	endpoint.RawQuery = query.Encode()
	var documents []DocumentSearchResult
	if err := c.documents.getJSON(ctx, endpoint.String(), &documents); err != nil {
		return nil, err
	}
	if len(documents) > limit {
		return documents[:limit], nil
	}
	return documents, nil
}

func (c *HTTPSourceClients) SearchKnowledge(ctx context.Context, queryText string, limit int) ([]KnowledgeSearchResult, error) {
	body, err := json.Marshal(struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{Query: queryText, Limit: limit})
	if err != nil {
		return nil, err
	}
	var response KnowledgeSearchResponse
	if err := c.knowledge.postJSON(ctx, c.knowledge.baseURL+"/v1/knowledge/search", body, &response); err != nil {
		return nil, err
	}
	return response.Results, nil
}

func (c sourceClient) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building source request: %w", err)
	}
	return c.doJSON(request, target)
}

func (c sourceClient) postJSON(ctx context.Context, endpoint string, body []byte, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building source request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return c.doJSON(request, target)
}

func (c sourceClient) doJSON(request *http.Request, target any) error {
	if c.baseURL == "" {
		return errors.New("source base url is not configured")
	}
	request.Header.Set("Accept", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrSourceNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("source returned %s: %s", resp.Status, errorMessage(resp))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding source response: %w", err)
	}
	return nil
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
