package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"den-services/shared/api"
	"den-services/shared/health"

	"den-services/mcp/internal/backend"
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
	toolProfileHeader   = "X-Den-MCP-Tool-Profile"
)

type toolProfileContextKey struct{}

type Handler struct {
	registry           *registry.Registry
	buildInfo          health.BuildInfo
	locator            *backend.Locator
	logger             *slog.Logger
	clock              func() time.Time
	detailReferenceTTL time.Duration
	detailReferenceKey []byte
	defaultToolProfile registry.ToolProfile
}

type HandlerOptions struct {
	Logger             *slog.Logger
	Clock              func() time.Time
	DetailReferenceTTL time.Duration
	DetailReferenceKey []byte
	DefaultToolProfile registry.ToolProfile
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
	ToolProfile     string `json:"toolProfile,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string                   `json:"protocolVersion"`
	Capabilities    serverCapabilities       `json:"capabilities"`
	ServerInfo      serverInfo               `json:"serverInfo"`
	Instructions    string                   `json:"instructions"`
	Catalog         registry.CatalogMetadata `json:"catalog"`
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
	Tools   []registry.ListedTool    `json:"tools"`
	Catalog registry.CatalogMetadata `json:"catalog"`
}

type toolsListParams struct {
	ToolProfile string `json:"toolProfile,omitempty"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolsCallResult struct {
	Content           []textContent   `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewMCPHandler(registry *registry.Registry, buildInfo health.BuildInfo, locator *backend.Locator) *Handler {
	return NewMCPHandlerWithOptions(registry, buildInfo, locator, HandlerOptions{})
}

func NewMCPHandlerWithOptions(toolRegistry *registry.Registry, buildInfo health.BuildInfo, locator *backend.Locator, options HandlerOptions) *Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.DetailReferenceTTL <= 0 {
		options.DetailReferenceTTL = 15 * time.Minute
	}
	defaultToolProfile, err := registry.ParseToolProfile(string(options.DefaultToolProfile))
	if err != nil {
		options.Logger.Error("invalid default MCP tool profile", "error", err)
		defaultToolProfile = registry.ToolProfileDirect
	}
	if len(options.DetailReferenceKey) == 0 {
		options.DetailReferenceKey = make([]byte, 32)
		if _, err := rand.Read(options.DetailReferenceKey); err != nil {
			options.Logger.Error("generating detail reference key", "error", err)
			fallback := sha256.Sum256([]byte(options.Clock().UTC().Format(time.RFC3339Nano)))
			options.DetailReferenceKey = fallback[:]
		}
	}
	return &Handler{
		registry:           toolRegistry,
		buildInfo:          buildInfo,
		locator:            locator,
		logger:             options.Logger,
		clock:              options.Clock,
		detailReferenceTTL: options.DetailReferenceTTL,
		detailReferenceKey: append([]byte(nil), options.DetailReferenceKey...),
		defaultToolProfile: defaultToolProfile,
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
	profile, err := h.requestToolProfile(r)
	if err != nil {
		writeRPCResponse(w, rpcErrorResponse(nil, errorInvalidParams, err.Error()))
		return
	}
	ctx := context.WithValue(r.Context(), toolProfileContextKey{}, profile)
	response, ok := h.handlePayload(ctx, body)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeRPCPayload(w, response)
}

func (h *Handler) handlePayload(ctx context.Context, body []byte) (any, bool) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return rpcErrorResponse(nil, errorParse, "parse error"), true
	}
	if body[0] != '[' {
		return h.handleBody(ctx, body)
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
		response, ok := h.handleBody(ctx, rawRequest)
		if ok {
			responses = append(responses, response)
		}
	}
	if len(responses) == 0 {
		return nil, false
	}
	return responses, true
}

func (h *Handler) handleBody(ctx context.Context, body []byte) (rpcResponse, bool) {
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
	return h.handleRequest(ctx, request), true
}

func (h *Handler) handleNotification(request rpcRequest) {
	if request.Method == methodInitialized {
		return
	}
}

func (h *Handler) handleRequest(ctx context.Context, request rpcRequest) rpcResponse {
	switch request.Method {
	case methodInitialize:
		profile, err := h.profileFromRequest(request.Params, profileFromContext(ctx))
		if err != nil {
			return rpcErrorResponse(request.ID, errorInvalidParams, err.Error())
		}
		return rpcResultResponse(request.ID, h.initialize(profile, request.Params))
	case methodToolsList:
		profile, err := h.profileFromRequest(request.Params, profileFromContext(ctx))
		if err != nil {
			return rpcErrorResponse(request.ID, errorInvalidParams, err.Error())
		}
		tools, err := h.registry.ToolsForProfile(profile)
		if err != nil {
			return rpcErrorResponse(request.ID, errorInternal, err.Error())
		}
		catalog, err := h.registry.Catalog(profile)
		if err != nil {
			return rpcErrorResponse(request.ID, errorInternal, err.Error())
		}
		return rpcResultResponse(request.ID, toolsListResult{Tools: tools, Catalog: catalog})
	case methodToolsCall:
		return h.toolsCall(ctx, request)
	default:
		return rpcErrorResponse(request.ID, errorMethodNotFound, fmt.Sprintf("method not found: %s", request.Method))
	}
}

func (h *Handler) initialize(profile registry.ToolProfile, rawParams json.RawMessage) initializeResult {
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
		Instructions: "Static Den MCP compatibility facade. Tool discovery is available before backend health is known. Discovery profile is advisory; registered service operations remain callable by trusted adapters.",
		Catalog:      mustCatalog(h.registry, profile),
	}
}

func (h *Handler) requestToolProfile(r *http.Request) (registry.ToolProfile, error) {
	return h.parseToolProfile(r.Header.Get(toolProfileHeader), h.defaultToolProfile)
}

func (h *Handler) profileFromRequest(rawParams json.RawMessage, fallback registry.ToolProfile) (registry.ToolProfile, error) {
	params := toolsListParams{}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return "", fmt.Errorf("invalid MCP tool profile parameters")
		}
	}
	return h.parseToolProfile(params.ToolProfile, fallback)
}

func (h *Handler) parseToolProfile(raw string, fallback registry.ToolProfile) (registry.ToolProfile, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return registry.ParseToolProfile(raw)
}

func profileFromContext(ctx context.Context) registry.ToolProfile {
	profile, ok := ctx.Value(toolProfileContextKey{}).(registry.ToolProfile)
	if !ok || profile == "" {
		return registry.ToolProfileDirect
	}
	return profile
}

func mustCatalog(toolRegistry *registry.Registry, profile registry.ToolProfile) registry.CatalogMetadata {
	catalog, err := toolRegistry.Catalog(profile)
	if err != nil {
		return registry.CatalogMetadata{Profile: profile, Revision: registry.ToolCatalogRevision}
	}
	return catalog
}

func (h *Handler) toolsCall(ctx context.Context, request rpcRequest) (response rpcResponse) {
	startedAt := h.clock()
	requestedTool := ""
	canonicalTool := ""
	backendName := ""
	outcome := "invalid_params"
	retryable := false
	defer func() {
		h.logger.Info("mcp_tool_call",
			"requested_tool", requestedTool,
			"canonical_tool", canonicalTool,
			"backend", backendName,
			"outcome", outcome,
			"retryable", retryable,
			"duration_ms", h.clock().Sub(startedAt).Milliseconds(),
		)
	}()

	params := toolsCallParams{}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return rpcErrorResponse(request.ID, errorInvalidParams, "invalid tools/call params")
	}
	requestedTool = params.Name
	tool, err := h.registry.Resolve(params.Name)
	if err != nil {
		if errors.Is(err, registry.ErrUnknownTool) {
			outcome = "unknown_tool"
			return rpcErrorResponse(request.ID, errorInvalidParams, err.Error())
		}
		outcome = "registry_error"
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	canonicalTool = tool.Name
	backendName = tool.Backend
	if tool.Operation == "get_details" {
		return h.getDetails(ctx, request, params, &outcome, &backendName, &retryable)
	}
	if tool.TombstoneMessage != "" {
		outcome = "retired"
		return rpcResultResponse(request.ID, retiredToolResult(tool))
	}
	if h.locator == nil {
		outcome = "unavailable"
		return rpcResultResponse(request.ID, errorToolResult(fmt.Sprintf("Tool %s is registered for backend %s, but backend proxy execution is not implemented yet.", tool.Name, tool.Backend), nil))
	}
	if _, routedBackend, resolveErr := h.locator.Resolve(tool.Operation); resolveErr == nil {
		backendName = routedBackend.Name
	}
	result, failure, err := h.locator.Call(ctx, backend.ToolCall{
		ToolName:  tool.Name,
		Operation: tool.Operation,
		Arguments: params.Arguments,
		RequestID: request.ID,
	})
	if err != nil {
		outcome = "rpc_error"
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	if failure != nil {
		outcome = "tool_error"
		retryable = failure.Retryable
		failureJSON := json.RawMessage(failure.Text())
		return rpcResultResponse(request.ID, errorToolResult(failure.Text(), failureJSON))
	}
	result.Value, err = h.attachDetailReference(tool.Name, params.Arguments, result.Value)
	if err != nil {
		outcome = "rpc_error"
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	outcome = "success"
	return rpcResultResponse(request.ID, json.RawMessage(result.Value))
}

func (h *Handler) getDetails(ctx context.Context, request rpcRequest, params toolsCallParams, outcome, backendName *string, retryable *bool) rpcResponse {
	var arguments getDetailsArguments
	if err := json.Unmarshal(params.Arguments, &arguments); err != nil || strings.TrimSpace(arguments.DetailRef) == "" {
		*outcome = "invalid_params"
		return rpcErrorResponse(request.ID, errorInvalidParams, "get_details requires detail_ref")
	}
	call, err := h.resolveDetailReference(strings.TrimSpace(arguments.DetailRef))
	if err != nil {
		*outcome = "invalid_detail_ref"
		return rpcResultResponse(request.ID, errorToolResult(err.Error(), nil))
	}
	tool, err := h.registry.Resolve(call.ToolName)
	if err != nil {
		*outcome = "registry_error"
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	*backendName = tool.Backend
	if h.locator == nil {
		*outcome = "unavailable"
		return rpcResultResponse(request.ID, errorToolResult("Detail backend proxy execution is unavailable.", nil))
	}
	if _, routedBackend, resolveErr := h.locator.Resolve(tool.Operation); resolveErr == nil {
		*backendName = routedBackend.Name
	}
	call.RequestID = request.ID
	result, failure, err := h.locator.Call(ctx, call)
	if err != nil {
		*outcome = "rpc_error"
		return rpcErrorResponse(request.ID, errorInternal, err.Error())
	}
	if failure != nil {
		*outcome = "tool_error"
		*retryable = failure.Retryable
		failureJSON := json.RawMessage(failure.Text())
		return rpcResultResponse(request.ID, errorToolResult(failure.Text(), failureJSON))
	}
	*outcome = "success"
	return rpcResultResponse(request.ID, json.RawMessage(result.Value))
}

func errorToolResult(message string, structured json.RawMessage) toolsCallResult {
	return toolsCallResult{
		Content: []textContent{
			{
				Type: "text",
				Text: message,
			},
		},
		IsError:           true,
		StructuredContent: structured,
	}
}

func retiredToolResult(tool registry.ToolDefinition) toolsCallResult {
	structured, _ := json.Marshal(map[string]any{
		"error":       "den_mcp_tool_retired",
		"tool":        tool.Name,
		"operation":   tool.Operation,
		"backend":     tool.Backend,
		"retired":     true,
		"retryable":   false,
		"suggested":   tool.TombstoneMessage,
		"hidden_from": "tools/list",
	})
	return errorToolResult(tool.TombstoneMessage, structured)
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
