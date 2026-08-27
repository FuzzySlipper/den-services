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

type boardRelayToolArguments struct {
	ProjectID  string `json:"project_id"`
	Visibility string `json:"visibility"`
}

func (c *Client) callBoardRelayREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildBoardRelayRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading board relay backend response: %w", err)
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

func buildBoardRelayRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	var arguments boardRelayToolArguments
	raw := call.Arguments
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return nil, fmt.Errorf("decoding board relay tool arguments: %w", err)
	}
	path := route.Path
	var body []byte
	switch route.Operation {
	case "sync_board_github":
		projectID := strings.TrimSpace(arguments.ProjectID)
		if projectID == "" {
			return nil, fmt.Errorf("board relay sync route requires project_id")
		}
		path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(projectID))
	case "set_board_github_visibility":
		if arguments.Visibility != "public" && arguments.Visibility != "private" {
			return nil, fmt.Errorf("board relay visibility route requires public or private visibility")
		}
		var err error
		body, err = json.Marshal(struct {
			Visibility string `json:"visibility"`
		}{arguments.Visibility})
		if err != nil {
			return nil, fmt.Errorf("encoding board relay visibility request: %w", err)
		}
	default:
		return nil, fmt.Errorf("%w: board relay operation %s", ErrUnsupportedAdapter, route.Operation)
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, backend.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building board relay backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}
