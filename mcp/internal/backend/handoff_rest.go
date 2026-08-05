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

type handoffToolArguments struct {
	Label        string `json:"label"`
	BodyMarkdown string `json:"body_markdown"`
}

type handoffSetBody struct {
	Label        string `json:"label"`
	BodyMarkdown string `json:"body_markdown"`
}

func (c *Client) callHandoffREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildHandoffRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading handoff backend response: %w", err)
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

func buildHandoffRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeHandoffToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := url.Parse(backend.BaseURL + route.Path)
	if err != nil {
		return nil, fmt.Errorf("parsing handoff backend URL: %w", err)
	}
	var body []byte
	switch route.Operation {
	case "set_handoff":
		body, err = json.Marshal(handoffSetBody{Label: strings.TrimSpace(arguments.Label), BodyMarkdown: arguments.BodyMarkdown})
	case "get_handoff":
		query := requestURL.Query()
		query.Set("label", strings.TrimSpace(arguments.Label))
		requestURL.RawQuery = query.Encode()
	default:
		return nil, fmt.Errorf("%w: handoff operation %s", ErrUnsupportedAdapter, route.Operation)
	}
	if err != nil {
		return nil, fmt.Errorf("encoding handoff request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building handoff backend request: %w", err)
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

func decodeHandoffToolArguments(raw json.RawMessage) (handoffToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments handoffToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return handoffToolArguments{}, fmt.Errorf("decoding handoff tool arguments: %w", err)
	}
	return arguments, nil
}
