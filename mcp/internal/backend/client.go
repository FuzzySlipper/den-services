package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"den-services/mcp/internal/config"
)

type Client struct {
	httpClient *http.Client
}

type backendRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  callParams      `json:"params"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type backendRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *backendRPCError `json:"error,omitempty"`
}

type backendRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Call(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	if route.RequestAdapter != RequestAdapterMCPToolsCall || route.ResponseAdapter != ResponseAdapterMCPJSONRPC {
		return Result{}, nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedAdapter, route.RequestAdapter, route.ResponseAdapter)
	}
	body, err := buildMCPToolCall(call)
	if err != nil {
		return Result{}, nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, backend.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, route.Method, backend.BaseURL+route.Path, bytes.NewReader(body))
	if err != nil {
		return Result{}, nil, fmt.Errorf("building backend request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Result{}, backendFailure(backend.Name, call.Operation, err, nil), nil
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, nil, fmt.Errorf("reading backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, response.StatusCode, responseBody), nil
	}
	var backendResponse backendRPCResponse
	if err := json.Unmarshal(responseBody, &backendResponse); err != nil {
		return Result{}, nil, fmt.Errorf("parsing backend JSON-RPC response: %w", err)
	}
	if backendResponse.Error != nil {
		return Result{}, nil, fmt.Errorf("backend JSON-RPC error %d: %s", backendResponse.Error.Code, backendResponse.Error.Message)
	}
	if len(backendResponse.Result) == 0 {
		return Result{}, nil, errors.New("backend JSON-RPC response missing result")
	}
	return Result{Value: backendResponse.Result}, nil, nil
}

func (c *Client) CheckReady(ctx context.Context, backend config.BackendConfig) *Failure {
	requestCtx, cancel := context.WithTimeout(ctx, backend.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, backend.BaseURL+backend.HealthPath, nil)
	if err != nil {
		return &Failure{
			Error:     "den_backend_config_error",
			Retryable: false,
			Backend:   backend.Name,
			Message:   err.Error(),
		}
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return backendFailure(backend.Name, "readiness", err, nil)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return statusFailure(backend.Name, "readiness", response.StatusCode, nil)
	}
	return nil
}

func buildMCPToolCall(call ToolCall) ([]byte, error) {
	arguments := call.Arguments
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	request := backendRPCRequest{
		JSONRPC: "2.0",
		ID:      call.RequestID,
		Method:  "tools/call",
		Params: callParams{
			Name:      call.ToolName,
			Arguments: arguments,
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding backend tools/call request: %w", err)
	}
	return data, nil
}

func backendFailure(backendName string, operation string, err error, statusCode *int) *Failure {
	return &Failure{
		Error:      "den_backend_unavailable",
		Retryable:  true,
		Backend:    backendName,
		Operation:  operation,
		Message:    classifyBackendError(err),
		StatusCode: statusCode,
	}
}

func statusFailure(backendName string, operation string, statusCode int, body []byte) *Failure {
	message := http.StatusText(statusCode)
	if len(body) > 0 {
		message = string(body)
	}
	return &Failure{
		Error:      statusFailureCode(statusCode),
		Retryable:  retryableStatus(statusCode),
		Backend:    backendName,
		Operation:  operation,
		Message:    message,
		StatusCode: &statusCode,
	}
}

func statusFailureCode(statusCode int) string {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return "den_backend_auth_failed"
	}
	if retryableStatus(statusCode) {
		return "den_backend_unavailable"
	}
	return "den_backend_error"
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout
}

func classifyBackendError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Den backend did not respond before the configured timeout. MCP transport is still healthy; retry after backend recovers."
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "Den backend request timed out. MCP transport is still healthy; retry after backend recovers."
	}
	return "Den backend is unavailable. MCP transport is still healthy; retry after backend recovers."
}
