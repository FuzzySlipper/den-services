package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	broker "den-services/playwright-broker/internal"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func runMCP(args []string) error {
	var cfgPath string
	flags := flag.NewFlagSet("den-playwright mcp", flag.ContinueOnError)
	flags.StringVar(&cfgPath, "config", os.Getenv(configPathEnv), "broker config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := loadBrokerConfig(cfgPath)
	if err != nil {
		return err
	}
	manager := broker.NewPlaytestManager(cfg)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request mcpRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			_ = encoder.Encode(mcpResponse{JSONRPC: "2.0", Error: &mcpError{Code: -32700, Message: err.Error()}})
			continue
		}
		response := handleMCPRequest(context.Background(), manager, request)
		if len(request.ID) > 0 {
			if err := encoder.Encode(response); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func handleMCPRequest(ctx context.Context, manager *broker.PlaytestManager, request mcpRequest) mcpResponse {
	response := mcpResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "den-playwright-playtest", "version": "1.0.0"},
			"instructions":    "Trusted-local permissive browser testing. Calls retain discrepancies and continue whenever possible; eval, selectors, injection, application readback, network data, and raw CDP are available.",
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": playtestTools()}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &mcpError{Code: -32602, Message: err.Error()}
			return response
		}
		value, err := callMCPTool(ctx, manager, params.Name, params.Arguments)
		if err != nil {
			response.Result = toolResult(map[string]any{"ok": false, "error": err.Error(), "continued": false}, true)
			return response
		}
		result := toolResult(value, false)
		if params.Name == "playtest_observe" {
			session, sessionErr := manager.Get(stringValue(params.Arguments["session_id"]))
			if sessionErr != nil {
				appendToolResultWarning(result, fmt.Sprintf("playtest image attachment unavailable: %v", sessionErr))
			} else {
				result = toolResultWithImages(value, false, session.ArtifactRoot, playtestObservationImagePaths(value))
			}
		}
		response.Result = result
	default:
		response.Error = &mcpError{Code: -32601, Message: "method not found"}
	}
	return response
}

func playtestTools() []map[string]any {
	openSchema := map[string]any{"type": "object", "additionalProperties": true}
	sessionSchema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"session_id": map[string]any{"type": "string"}},
		"required":             []string{"session_id"},
		"additionalProperties": true,
	}
	return []map[string]any{
		{
			"name":        "playtest_start",
			"description": "Start a permissive persistent Playwright session. Set verbose_trace=true for optional concise decision tracing; all extra fields are retained as evidence metadata.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project":       map[string]any{"type": "string"},
					"repo_root":     map[string]any{"type": "string"},
					"manifest_path": map[string]any{"type": "string"},
					"owner":         map[string]any{"type": "string"},
					"scenario":      map[string]any{"type": "string"},
					"headed":        map[string]any{"type": "boolean"},
					"record_video":  map[string]any{"type": "boolean"},
					"viewport":      map[string]any{"type": "object", "additionalProperties": true},
				},
				"required":             []string{"project"},
				"additionalProperties": true,
			},
		},
		{"name": "playtest_observe", "description": "Capture screenshots/frame bursts and any requested state. Verbose sessions may attach a short trace verification/update summary.", "inputSchema": sessionSchema},
		{"name": "playtest_act", "description": "Run typed browser actions or unrestricted Playwright evaluation, injection, request, and raw CDP actions; errors are logged and later actions continue. Verbose sessions may attach short observe, hypothesis, intent, and expected-effect trace summaries.", "inputSchema": sessionSchema},
		{"name": "playtest_inspect", "description": "Read arbitrary DOM, application, renderer, storage, network, or CDP state from a live session.", "inputSchema": sessionSchema},
		{"name": "playtest_finish", "description": "Finalize evidence and best-effort cleanup. Optional neutral_observation, operational_outcome, and acceptance_mapping fields are retained independently; exit_interview records tester difficulties, workarounds, confidence, and suggestions.", "inputSchema": sessionSchema},
		{"name": "playtest_cancel", "description": "Cancel a session, finalize partial evidence, and attempt cleanup.", "inputSchema": sessionSchema},
		{"name": "playtest_get", "description": "Get the persisted local session record.", "inputSchema": sessionSchema},
		{"name": "playtest_list", "description": "List persisted local playtest session records.", "inputSchema": openSchema},
	}
}

func callMCPTool(ctx context.Context, manager *broker.PlaytestManager, name string, arguments map[string]any) (any, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	switch name {
	case "playtest_start":
		project, _ := arguments["project"].(string)
		metadata := copyAnyMap(arguments)
		options := broker.PlaytestStartOptions{
			Project:      project,
			RepoRoot:     stringValue(arguments["repo_root"]),
			ManifestPath: stringValue(arguments["manifest_path"]),
			Owner:        stringValue(arguments["owner"]),
			Scenario:     stringValue(arguments["scenario"]),
			DenProjectID: stringValue(arguments["den_project_id"]),
			DenTaskID:    int64(numberValue(arguments["den_task_id"])),
			Metadata:     metadata,
		}
		if headed, ok := arguments["headed"].(bool); ok {
			options.Headed = &headed
		}
		if video, ok := arguments["record_video"].(bool); ok {
			options.RecordVideo = &video
		}
		if viewport, ok := arguments["viewport"].(map[string]any); ok {
			parsed := broker.Viewport{Width: numberValue(viewport["width"]), Height: numberValue(viewport["height"])}
			options.Viewport = &parsed
		}
		return manager.Start(ctx, options)
	case "playtest_get":
		return manager.Get(stringValue(arguments["session_id"]))
	case "playtest_list":
		return manager.List()
	case "playtest_observe", "playtest_act", "playtest_inspect", "playtest_finish", "playtest_cancel":
		sessionID := stringValue(arguments["session_id"])
		if strings.TrimSpace(sessionID) == "" {
			return nil, errors.New("session_id is required")
		}
		arguments["kind"] = strings.TrimPrefix(name, "playtest_")
		return manager.Call(ctx, sessionID, arguments)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func toolResult(value any, isError bool) map[string]any {
	data, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(data)}},
		"structuredContent": value,
		"isError":           isError,
	}
}

func toolResultWithImages(value any, isError bool, artifactRoot string, relativePaths []string) map[string]any {
	result := toolResult(value, isError)
	content := result["content"].([]map[string]any)
	const maxImages = 16
	const maxTotalBytes = 32 * 1024 * 1024
	totalBytes := 0
	warnings := []string{}
	for index, relativePath := range relativePaths {
		if index >= maxImages {
			warnings = append(warnings, fmt.Sprintf("omitted %d image(s) after attachment limit %d", len(relativePaths)-index, maxImages))
			break
		}
		data, err := readBoundedArtifact(artifactRoot, relativePath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", relativePath, err))
			continue
		}
		if totalBytes+len(data) > maxTotalBytes {
			warnings = append(warnings, fmt.Sprintf("%s: omitted after %d-byte attachment limit", relativePath, maxTotalBytes))
			continue
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			warnings = append(warnings, fmt.Sprintf("%s: unsupported content type %s", relativePath, mimeType))
			continue
		}
		totalBytes += len(data)
		content = append(content, map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(data),
			"mimeType": mimeType,
		})
	}
	result["content"] = content
	for _, warning := range warnings {
		appendToolResultWarning(result, "playtest image attachment warning: "+warning)
	}
	return result
}

func playtestObservationImagePaths(value any) []string {
	response, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	observation, ok := response["result"].(map[string]any)
	if !ok {
		return nil
	}
	paths := []string{}
	if screenshot, ok := observation["screenshot"].(string); ok && strings.TrimSpace(screenshot) != "" {
		paths = append(paths, screenshot)
	}
	if frames, ok := observation["frames"].([]any); ok {
		for _, frame := range frames {
			if path, ok := frame.(string); ok && strings.TrimSpace(path) != "" {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func readBoundedArtifact(artifactRoot string, relativePath string) ([]byte, error) {
	localPath := filepath.Clean(filepath.FromSlash(relativePath))
	if !filepath.IsLocal(localPath) {
		return nil, errors.New("path is outside the session artifact root")
	}
	root, err := os.OpenRoot(artifactRoot)
	if err != nil {
		return nil, fmt.Errorf("opening session artifact root: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading artifact within session root: %w", err)
	}
	return data, nil
}

func appendToolResultWarning(result map[string]any, warning string) {
	content, _ := result["content"].([]map[string]any)
	result["content"] = append(content, map[string]any{"type": "text", "text": warning})
}

func copyAnyMap(source map[string]any) map[string]any {
	target := make(map[string]any, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func numberValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}
