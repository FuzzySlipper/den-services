package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// The Resources service records what is using the workshop machines: host
// telemetry, the shared resource directory and advisory work check-ins.
// Contract: docs/resources-api-contract.md.

// defaultResourcesURL is web-edge on the Den service host; it forwards
// /api/v1/resources to Gateway and on to the Resources service.
const defaultResourcesURL = "http://192.168.1.5:18080/api"

const resourcesRequestTimeout = 10 * time.Second

type ResourcesClient struct {
	baseURL    string
	httpClient *http.Client
}

// ResourcesHTTPError is a response the service sent; its message is the
// service's own explanation, such as the list of known hosts.
type ResourcesHTTPError struct {
	StatusCode int
	Message    string
}

func (e *ResourcesHTTPError) Error() string {
	return fmt.Sprintf("resources service returned HTTP %d: %s", e.StatusCode, e.Message)
}

func NewResourcesClient(baseURL string) (*ResourcesClient, error) {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(normalized)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("resources URL %q must be an http(s) URL with a host", baseURL)
	}
	return &ResourcesClient{baseURL: normalized, httpClient: &http.Client{Timeout: resourcesRequestTimeout}}, nil
}

// resourcesURL picks the base URL: --url, then DEN_RESOURCES_URL, then the
// LAN default.
func resourcesURL(flag string) string {
	if flag != "" {
		return flag
	}
	if value := strings.TrimSpace(os.Getenv("DEN_RESOURCES_URL")); value != "" {
		return value
	}
	return defaultResourcesURL
}

// Do sends one request to a path such as /v1/resources/state and returns the
// response body unchanged.
func (c *ResourcesClient) Do(ctx context.Context, method string, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build resources request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("resources service unreachable: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read resources response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &ResourcesHTTPError{StatusCode: response.StatusCode, Message: serviceErrorMessage(payload)}
	}
	return payload, nil
}

func serviceErrorMessage(payload []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &envelope) == nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	message := strings.TrimSpace(string(payload))
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	return message
}

func resourcesPath(segments ...string) string {
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return "/v1/resources/" + strings.Join(escaped, "/")
}
