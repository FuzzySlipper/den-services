package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const defaultBoardRelayURL = "http://127.0.0.1:8101"

type BoardRelayClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func BoardRelayClientFromEnv() (*BoardRelayClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("DEN_BOARD_RELAY_URL"))
	if baseURL == "" {
		baseURL = defaultBoardRelayURL
	}
	return NewBoardRelayClient(baseURL, os.Getenv("DEN_BOARD_RELAY_SERVICE_TOKEN"), http.DefaultClient)
}

func NewBoardRelayClient(baseURL string, token string, client *http.Client) (*BoardRelayClient, error) {
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("board relay URL must use http or https")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &BoardRelayClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: client}, nil
}

func (c *BoardRelayClient) Sync(ctx context.Context, projectID string) ([]byte, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("board github-sync requires project")
	}
	return c.request(ctx, http.MethodPost, "/v1/projects/"+projectID+"/board/github-sync", nil)
}

func (c *BoardRelayClient) SetVisibility(ctx context.Context, visibility string) ([]byte, error) {
	if visibility != "public" && visibility != "private" {
		return nil, fmt.Errorf("board github-visibility requires public or private visibility")
	}
	return c.request(ctx, http.MethodPatch, "/v1/board/github-visibility", []byte(`{"visibility":"`+visibility+`"}`))
}

func (c *BoardRelayClient) request(ctx context.Context, method string, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building board relay request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling board relay: %w", err)
	}
	defer response.Body.Close()
	responseBody := new(bytes.Buffer)
	if _, err := responseBody.ReadFrom(response.Body); err != nil {
		return nil, fmt.Errorf("reading board relay response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("board relay returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(responseBody.String()))
	}
	return responseBody.Bytes(), nil
}
