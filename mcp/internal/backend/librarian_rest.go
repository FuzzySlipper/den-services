package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"den-services/mcp/internal/config"
)

type librarianToolArguments struct {
	ProjectID     string          `json:"project_id"`
	Query         string          `json:"query"`
	TaskID        *int64          `json:"task_id"`
	IncludeGlobal *bool           `json:"include_global"`
	SourceLimits  json.RawMessage `json:"source_limits"`
}

type librarianQueryBody struct {
	Query         string          `json:"query"`
	TaskID        *int64          `json:"task_id,omitempty"`
	IncludeGlobal *bool           `json:"include_global,omitempty"`
	SourceLimits  json.RawMessage `json:"source_limits,omitempty"`
}

func (c *Client) callLibrarianREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildLibrarianRESTRequest(ctx, backend, route, call)
	if err != nil {
		return Result{}, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return Result{}, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, nil, fmt.Errorf("reading librarian backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func buildLibrarianRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeLibrarianToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := librarianRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := librarianRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building librarian backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func decodeLibrarianToolArguments(raw json.RawMessage) (librarianToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments librarianToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return librarianToolArguments{}, fmt.Errorf("decoding librarian tool arguments: %w", err)
	}
	return arguments, nil
}

func librarianRESTRequestBody(operation string, arguments librarianToolArguments) ([]byte, error) {
	switch operation {
	case "query_librarian":
		return json.Marshal(librarianQueryBody{
			Query:         strings.TrimSpace(arguments.Query),
			TaskID:        arguments.TaskID,
			IncludeGlobal: arguments.IncludeGlobal,
			SourceLimits:  compactRaw(arguments.SourceLimits),
		})
	default:
		return nil, fmt.Errorf("%w: librarian operation %s", ErrUnsupportedAdapter, operation)
	}
}

func librarianRESTURL(baseURL string, route Route, arguments librarianToolArguments) (string, error) {
	routePath, err := expandLibrarianPath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing librarian backend URL: %w", err)
	}
	return parsedURL.String(), nil
}

func expandLibrarianPath(path string, arguments librarianToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("librarian route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	return result, nil
}
