package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"den-services/mcp/internal/config"
)

type Client struct {
	httpClient *http.Client
	mu         sync.Mutex
	sessions   map[string]string
}

type backendRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  callParams      `json:"params"`
}

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	return &Client{
		httpClient: httpClient,
		sessions:   make(map[string]string),
	}
}

func (c *Client) Call(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	switch {
	case route.RequestAdapter == RequestAdapterMCPToolsCall && route.ResponseAdapter == ResponseAdapterMCPJSONRPC:
		return c.callMCPTool(ctx, backend, route, call)
	case route.RequestAdapter == RequestAdapterMCPProjectsREST && route.ResponseAdapter == ResponseAdapterMCPToolResultJSON:
		return c.callProjectsREST(ctx, backend, route, call)
	default:
		return Result{}, nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedAdapter, route.RequestAdapter, route.ResponseAdapter)
	}
}

func (c *Client) callMCPTool(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	body, err := buildMCPToolCall(call)
	if err != nil {
		return Result{}, nil, err
	}

	responseBody, failure, err := c.doRPC(ctx, backend, route, call, body, c.session(backend.Name))
	if err != nil {
		return Result{}, nil, err
	}
	if failure != nil && sessionRequiredFailure(failure) {
		sessionID, sessionFailure, err := c.initializeSession(ctx, backend, route)
		if err != nil || sessionFailure != nil {
			return Result{}, sessionFailure, err
		}
		c.setSession(backend.Name, sessionID)
		responseBody, failure, err = c.doRPC(ctx, backend, route, call, body, sessionID)
		if err != nil {
			return Result{}, nil, err
		}
	}
	if failure != nil {
		return Result{}, failure, nil
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

func (c *Client) doRPC(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall, body []byte, sessionID string) ([]byte, *Failure, error) {
	response, cancel, err := c.doBackendRequest(ctx, backend, route, body, sessionID)
	if err != nil {
		return nil, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := readMCPResponseBody(response)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	return responseBody, nil, nil
}

func (c *Client) initializeSession(ctx context.Context, backend config.BackendConfig, route Route) (string, *Failure, error) {
	body, err := json.Marshal(struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      string           `json:"id"`
		Method  string           `json:"method"`
		Params  initializeParams `json:"params"`
	}{
		JSONRPC: "2.0",
		ID:      "den-services-mcp-backend-init",
		Method:  "initialize",
		Params: initializeParams{
			ProtocolVersion: "2025-11-25",
			Capabilities:    map[string]any{},
			ClientInfo: clientInfo{
				Name:    "den-services-mcp",
				Version: "dev",
			},
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("encoding backend initialize request: %w", err)
	}
	response, cancel, err := c.doBackendRequest(ctx, backend, route, body, "")
	if err != nil {
		return "", backendFailure(backend.Name, "initialize", "", err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := readMCPResponseBody(response)
	if err != nil {
		return "", nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", statusFailure(backend.Name, "initialize", "", response.StatusCode, responseBody), nil
	}
	sessionID := strings.TrimSpace(response.Header.Get("Mcp-Session-Id"))
	if sessionID == "" {
		return "", nil, errors.New("backend streamable MCP initialize response missing Mcp-Session-Id")
	}
	return sessionID, nil, nil
}

func (c *Client) doBackendRequest(ctx context.Context, backend config.BackendConfig, route Route, body []byte, sessionID string) (*http.Response, context.CancelFunc, error) {
	requestCtx, cancel := context.WithTimeout(ctx, backend.Timeout)
	request, err := http.NewRequestWithContext(requestCtx, route.Method, backend.BaseURL+route.Path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("building backend request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return response, cancel, nil
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
		return backendFailure(backend.Name, "readiness", "", err, nil)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return statusFailure(backend.Name, "readiness", "", response.StatusCode, nil)
	}
	return nil
}

func (c *Client) session(backendName string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[backendName]
}

func (c *Client) setSession(backendName string, sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[backendName] = sessionID
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

func backendFailure(backendName string, operation string, toolName string, err error, statusCode *int) *Failure {
	return &Failure{
		Error:      "den_backend_unavailable",
		Retryable:  true,
		Backend:    backendName,
		Operation:  operation,
		Tool:       toolName,
		Message:    classifyBackendError(err),
		StatusCode: statusCode,
	}
}

func statusFailure(backendName string, operation string, toolName string, statusCode int, body []byte) *Failure {
	message := statusFailureMessage(statusCode, body)
	return &Failure{
		Error:      statusFailureCode(statusCode),
		Retryable:  retryableStatus(statusCode),
		Backend:    backendName,
		Operation:  operation,
		Tool:       toolName,
		Message:    message,
		StatusCode: &statusCode,
	}
}

func statusFailureMessage(statusCode int, body []byte) string {
	bodyText := string(body)
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		if bodyText == "" {
			return "Den backend rejected MCP service credentials; check backend service_token_env and token configuration."
		}
		return bodyText + " Check backend service_token_env and token configuration."
	}
	if bodyText != "" {
		return bodyText
	}
	return http.StatusText(statusCode)
}

func statusFailureCode(statusCode int) string {
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return "den_backend_auth_failed"
	}
	if statusCode == http.StatusBadRequest || statusCode == http.StatusNotFound {
		return "den_backend_request_failed"
	}
	if retryableStatus(statusCode) {
		return "den_backend_unavailable"
	}
	return "den_backend_error"
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout
}

func sessionRequiredFailure(failure *Failure) bool {
	if failure == nil || failure.StatusCode == nil {
		return false
	}
	if *failure.StatusCode != http.StatusBadRequest && *failure.StatusCode != http.StatusNotAcceptable {
		return false
	}
	message := strings.ToLower(failure.Message)
	return strings.Contains(message, "mcp-session-id") ||
		strings.Contains(message, "new session") ||
		strings.Contains(message, "text/event-stream")
}

func readMCPResponseBody(response *http.Response) ([]byte, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("reading backend response: %w", err)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return body, nil
	}
	data, ok := firstSSEData(body)
	if !ok {
		return nil, errors.New("backend streamable MCP response missing message data")
	}
	return data, nil
}

func firstSSEData(body []byte) ([]byte, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) > 0 {
				return []byte(strings.Join(dataLines, "\n")), true
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) > 0 {
		return []byte(strings.Join(dataLines, "\n")), true
	}
	return nil, false
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
