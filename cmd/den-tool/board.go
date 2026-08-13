package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultBoardURL = "http://127.0.0.1:8100"

type BoardSearchOptions struct {
	ProjectID string
	Query     string
	AfterID   *int64
	Limit     *int
}

type BoardClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

type BoardHTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *BoardHTTPError) Error() string {
	message := strings.TrimSpace(string(e.Body))
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	if message == "" {
		return fmt.Sprintf("board service returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("board service returned HTTP %d: %s", e.StatusCode, message)
}

func NewBoardClient(baseURL, serviceToken string, httpClient *http.Client) (*BoardClient, error) {
	normalizedBaseURL := strings.TrimSpace(baseURL)
	parsed, err := url.Parse(normalizedBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse board URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("board URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("board URL must include a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("board URL must not include a query or fragment")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &BoardClient{
		baseURL:      strings.TrimRight(normalizedBaseURL, "/"),
		serviceToken: serviceToken,
		httpClient:   httpClient,
	}, nil
}

func BoardClientFromEnv() (*BoardClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("DEN_BOARD_URL"))
	if baseURL == "" {
		baseURL = defaultBoardURL
	}
	return NewBoardClient(baseURL, os.Getenv("DEN_BOARD_SERVICE_TOKEN"), http.DefaultClient)
}

func (c *BoardClient) Search(ctx context.Context, options BoardSearchOptions) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("board client is nil")
	}
	if strings.TrimSpace(options.ProjectID) == "" {
		return nil, fmt.Errorf("board project is required")
	}
	if strings.TrimSpace(options.Query) == "" {
		return nil, fmt.Errorf("board query is required")
	}
	if options.AfterID != nil && *options.AfterID < 0 {
		return nil, fmt.Errorf("board after-id must be non-negative")
	}
	if options.Limit != nil && *options.Limit <= 0 {
		return nil, fmt.Errorf("board limit must be positive")
	}

	requestURL, err := c.searchURL(options)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create board request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if c.serviceToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request board search: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read board response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &BoardHTTPError{StatusCode: response.StatusCode, Body: body}
	}
	return body, nil
}

func (c *BoardClient) searchURL(options BoardSearchOptions) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse board URL: %w", err)
	}
	path := strings.TrimRight(parsed.Path, "/")
	escapedPath := strings.TrimRight(parsed.EscapedPath(), "/")
	segments := []string{"v1", "projects", options.ProjectID, "board", "posts", "search"}
	for _, segment := range segments {
		path += "/" + segment
		escapedPath += "/" + url.PathEscape(segment)
	}
	parsed.Path = path
	if escapedPath != path {
		parsed.RawPath = escapedPath
	} else {
		parsed.RawPath = ""
	}

	queryParts := []string{"q=" + url.QueryEscape(options.Query)}
	if options.AfterID != nil {
		queryParts = append(queryParts, "after_id="+strconv.FormatInt(*options.AfterID, 10))
	}
	if options.Limit != nil {
		queryParts = append(queryParts, "limit="+strconv.Itoa(*options.Limit))
	}
	parsed.RawQuery = strings.Join(queryParts, "&")
	return parsed.String(), nil
}
