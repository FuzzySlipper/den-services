package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"

	"den-services/mcp/internal/registry"
)

const (
	jsonRPCVersion      = "2.0"
	latestProtocol      = "2025-11-25"
	methodInitialize    = "initialize"
	methodToolsList     = "tools/list"
	methodToolsCall     = "tools/call"
	methodInitialized   = "notifications/initialized"
	errorParse          = -32700
	errorInvalidRequest = -32600
	errorMethodNotFound = -32601
	errorInvalidParams  = -32602
	errorInternal       = -32603
)

type Handler struct {
	registry  *registry.Registry
	buildInfo health.BuildInfo
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions"`
}

type serverCapabilities struct {
	Tools toolsCapability `json:"tools"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []registry.ListedTool `json:"tools"`
}

type toolsCallParams struct {
	Name string `json:"name"`
}

type toolsCallResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewMCPHandler(registry *registry.Registry, buildInfo health.BuildInfo) *Handler {
	return &Handler{
		registry:  registry,
		buildInfo: buildInfo,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "MCP endpoint accepts POST only")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCResponse(w, rpcErrorResponse(nil, errorInternal, "reading request body"))
		return
	}
	response, ok := h.handlePayload(body)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeRPCPayload(w, response)
}

func (h *Handler) handlePayload(body []byte) (any, bool) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return rpcErrorResponse(nil, errorParse, "parse error"), true
	}
	if body[0] != '[' {
		return h.handleBody(body)
	}
	var rawRequests []json.RawMessage
	if err := json.Unmarshal(body, &rawRequests); err != nil {
		return rpcErrorResponse(nil, errorParse, "parse error"), true
	}
	if len(rawRequests) == 0 {
		return rpcErrorResponse(nil, errorInvalidRequest, "invalid JSON-RPC batch"), true
	}
	responses := make([]rpcResponse, 0, len(rawRequests))
	for _, rawRequest := range rawRequests {
		response, ok := h.handleBody(rawRequest)
		if ok {
			responses = append(responses, response)
		}
	}
	if len(responses) == 0 {
		return nil, false
	}
	return responses, true
}

func (h *Handler) handleBody(body []byte) (rpcResponse, bool) {
	var request rpcRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return rpcErrorResponse(nil, errorParse, "parse error"), true
	}
	if request.ID == nil {
		h.handleNotification(request)
		return rpcResponse{}, false
	}
	if request.JSONRPC != jsonRPCVersion || request.Method == "" {
		return rpcErrorResponse(request.ID, errorInvalidRequest, "invalid JSON-RPC request"), true
	}
	return h.handleRequest(request), true
}

func (h *Handler) handleNotification(request rpcRequest) {
	if request.Method == methodInitialized {
		return
	}
}

func (h *Handler) handleRequest(request rpcRequest) rpcResponse {
	switch request.Method {
	case methodInitialize:
		return rpcResultResponse(request.ID, h.initialize(request.Params))
	case methodToolsList:
		return rpcResultResponse(request.ID, toolsListResult{Tools: h.registry.Tools()})
	case methodToolsCall:
		return h.toolsCall(request)
	default:
		return rpcErrorResponse(request.ID, errorMethodNotFound, fmt.Sprintf("method not found: %s", request.Method))
	}
}

func (h *Handler) initialize(rawParams json.RawMessage) initializeResult {
	params := initializeParams{}
	if len(rawParams) > 0 {
		_ = json.Unmarshal(rawParams, &params)
	}
	return initializeResult{
		ProtocolVersion: negotiatedProtocol(params.ProtocolVersion),
		Capabilities: serverCapabilities{
			Tools: toolsCapability{ListChanged: false},
		},
		ServerInfo: serverInfo{
			Name:    "den-services-mcp",
			Title:   "Den Services MCP",
			Version: h.buildInfo.Version,
		},
		Instructions: "Static Den MCP compatibility facade. Tool discovery is available before backend health is known.",
	}
}

func (h *Handler) toolsCall(request rpcRequest) rpcResponse {
	params := toolsCallParams{}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return rpcErrorResponse(request.ID, errorInvalidParams, "invalid tools/call params")
	}
	tool, err := h.registry.Resolve(params.Name)
	if err != nil {
		if errors.Is(err, registry.ErrUnknownTool) {
			return rpcErrorResponse(request.ID, errorInvalidParams, err.Error())
		}
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	return rpcResultResponse(request.ID, toolsCallResult{
		Content: []textContent{
			{
				Type: "text",
				Text: fmt.Sprintf("Tool %s is registered for backend %s, but backend proxy execution is not implemented yet.", tool.Name, tool.Backend),
			},
		},
		IsError: true,
	})
}

func negotiatedProtocol(requested string) string {
	for _, supported := range []string{latestProtocol, "2025-06-18", "2025-03-26", "2024-11-05"} {
		if requested == supported {
			return requested
		}
	}
	return latestProtocol
}

func rpcResultResponse(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	}
}

func rpcErrorResponse(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	}
}

func writeRPCResponse(w http.ResponseWriter, response rpcResponse) {
	api.WriteJSON(w, http.StatusOK, response)
}

func writeRPCPayload(w http.ResponseWriter, payload any) {
	api.WriteJSON(w, http.StatusOK, payload)
}
