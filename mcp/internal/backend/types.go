package backend

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	RequestAdapterMCPToolsCall             = "mcp_tools_call"
	RequestAdapterMCPProjectsREST          = "mcp_projects_rest"
	RequestAdapterMCPTasksREST             = "mcp_tasks_rest"
	RequestAdapterMCPMessagesREST          = "mcp_messages_rest"
	RequestAdapterMCPDocumentsREST         = "mcp_documents_rest"
	RequestAdapterMCPReviewREST            = "mcp_review_rest"
	ResponseAdapterMCPJSONRPC              = "mcp_jsonrpc_result"
	ResponseAdapterMCPToolResultJSON       = "mcp_tool_result_json"
	StateReady                       State = "ready"
	StateUnavailable                 State = "unavailable"
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
	Error        string `json:"error"`
	Retryable    bool   `json:"retryable"`
	Backend      string `json:"backend"`
	Operation    string `json:"operation"`
	Tool         string `json:"tool,omitempty"`
	Message      string `json:"message"`
	StatusCode   *int   `json:"status_code"`
	CircuitState string `json:"circuit_state,omitempty"`
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
