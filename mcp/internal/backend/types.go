package backend

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	RequestAdapterMCPToolsCall       = "mcp_tools_call"
	ResponseAdapterMCPJSONRPC        = "mcp_jsonrpc_result"
	StateReady                 State = "ready"
	StateUnavailable           State = "unavailable"
)

type State string

type Route struct {
	Operation       string
	Backend         string
	Method          string
	Path            string
	RequestAdapter  string
	ResponseAdapter string
}

type ToolCall struct {
	ToolName  string
	Operation string
	Arguments json.RawMessage
	RequestID json.RawMessage
}

type Result struct {
	Value json.RawMessage
}

type Failure struct {
	Error      string `json:"error"`
	Retryable  bool   `json:"retryable"`
	Backend    string `json:"backend"`
	Operation  string `json:"operation"`
	Message    string `json:"message"`
	StatusCode *int   `json:"status_code"`
}

var (
	ErrRouteNotFound      = errors.New("route not found")
	ErrBackendNotFound    = errors.New("backend not found")
	ErrUnsupportedAdapter = errors.New("unsupported route adapter")
	ErrBackendUnavailable = errors.New("backend unavailable")
)

func (f Failure) Text() string {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s","retryable":%t,"backend":"%s","operation":"%s","message":"%s"}`, f.Error, f.Retryable, f.Backend, f.Operation, f.Message)
	}
	return string(data)
}
