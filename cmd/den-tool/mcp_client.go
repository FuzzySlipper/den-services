package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultMCPURL       = "http://192.168.1.10:5199/mcp"
	maxMCPResponseBytes = 1024 * 1024
)

type MCPClient struct {
	endpoint string
	token    string
	client   *http.Client
}

type mcpRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type mcpToolEnvelope struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

func MCPClientFromEnv() (*MCPClient, error) {
	endpoint := strings.TrimSpace(os.Getenv("DEN_MCP_URL"))
	if endpoint == "" {
		endpoint = defaultMCPURL
	}
	return NewMCPClient(endpoint, os.Getenv("DEN_MCP_TOKEN"), &http.Client{Timeout: 65 * time.Second})
}

func NewMCPClient(endpoint, token string, client *http.Client) (*MCPClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("den MCP URL must be an absolute http or https URL")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("den MCP URL must not contain a fragment")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &MCPClient{endpoint: parsed.String(), token: token, client: client}, nil
}

func (c *MCPClient) Call(ctx context.Context, operation string, arguments json.RawMessage) (json.RawMessage, bool, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "den-tool", "method": "tools/call",
		"params": map[string]any{"name": operation, "arguments": arguments},
	})
	if err != nil {
		return nil, false, fmt.Errorf("encode MCP request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build MCP request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, false, fmt.Errorf("call Den MCP: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	limited := io.LimitReader(response.Body, maxMCPResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, fmt.Errorf("read Den MCP response: %w", err)
	}
	if len(responseBody) > maxMCPResponseBytes {
		return nil, false, fmt.Errorf("den MCP response exceeded %d bytes", maxMCPResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("den MCP returned HTTP %d: %s", response.StatusCode, boundedText(responseBody, 4096))
	}
	var rpc mcpRPCResponse
	if err := json.Unmarshal(responseBody, &rpc); err != nil {
		return nil, false, fmt.Errorf("decode Den MCP response: %w", err)
	}
	if rpc.Error != nil {
		return nil, false, fmt.Errorf("den MCP JSON-RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if len(rpc.Result) == 0 || !json.Valid(rpc.Result) {
		return nil, false, fmt.Errorf("den MCP response is missing a valid result")
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(rpc.Result, &result); err != nil {
		return nil, false, fmt.Errorf("decode Den MCP result: %w", err)
	}
	return rpc.Result, result.IsError, nil
}

func (c *MCPClient) CallStructured(ctx context.Context, operation string, arguments json.RawMessage) (json.RawMessage, error) {
	result, isError, err := c.Call(ctx, operation, arguments)
	if err != nil {
		return nil, err
	}
	var envelope mcpToolEnvelope
	if err := json.Unmarshal(result, &envelope); err != nil {
		return nil, fmt.Errorf("decode Den operation %s result: %w", operation, err)
	}
	if isError || envelope.IsError {
		message := "operation returned an error"
		if len(envelope.Content) > 0 && strings.TrimSpace(envelope.Content[0].Text) != "" {
			message = boundedText([]byte(envelope.Content[0].Text), 4096)
		}
		return nil, fmt.Errorf("den operation %s: %s", operation, message)
	}
	if len(envelope.StructuredContent) == 0 || !json.Valid(envelope.StructuredContent) {
		return nil, fmt.Errorf("den operation %s returned no structured JSON", operation)
	}
	return envelope.StructuredContent, nil
}

func boundedText(data []byte, limit int) string {
	text := strings.TrimSpace(string(data))
	if len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}
