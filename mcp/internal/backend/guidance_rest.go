package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"den-services/mcp/internal/config"
)

type guidanceToolArguments struct {
	ProjectID         string          `json:"project_id"`
	EntryID           int64           `json:"entry_id"`
	DocumentProjectID string          `json:"document_project_id"`
	DocumentSlug      string          `json:"document_slug"`
	Importance        string          `json:"importance"`
	Audience          json.RawMessage `json:"audience"`
	SortOrder         int             `json:"sort_order"`
	Notes             string          `json:"notes"`
	IncludeGlobal     bool            `json:"include_global"`
	MaxBytes          int             `json:"max_bytes"`
	IncludeContent    *bool           `json:"include_content"`
}

type guidanceEntryBody struct {
	DocumentProjectID string   `json:"document_project_id,omitempty"`
	DocumentSlug      string   `json:"document_slug"`
	Importance        string   `json:"importance,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	SortOrder         int      `json:"sort_order,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

func (c *Client) callGuidanceREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildGuidanceRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading guidance backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	compatibleBody, err := guidanceCompatibleResponseBody(route.Operation, responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	result, err := buildRESTToolResult(compatibleBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

type guidancePacketResponse struct {
	ProjectID       string                   `json:"project_id"`
	ResolvedAt      string                   `json:"resolved_at"`
	Sources         []guidanceSourceResponse `json:"sources"`
	SkippedSources  json.RawMessage          `json:"skipped_sources,omitempty"`
	ContentMarkdown string                   `json:"content_markdown"`
	ContentSHA256   string                   `json:"content_sha256,omitempty"`
	ContentBytes    int                      `json:"content_bytes,omitempty"`
	Truncated       bool                     `json:"truncated,omitempty"`
	Incomplete      bool                     `json:"incomplete,omitempty"`
}

type guidanceSourceResponse struct {
	EntryID           int64    `json:"entry_id"`
	SourceScope       string   `json:"source_scope"`
	DocumentProjectID string   `json:"document_project_id"`
	DocumentSlug      string   `json:"document_slug"`
	DocumentTitle     string   `json:"document_title"`
	DocumentType      string   `json:"document_type"`
	DocumentUpdatedAt string   `json:"document_updated_at"`
	Visibility        string   `json:"visibility"`
	Tags              []string `json:"tags,omitempty"`
	Importance        string   `json:"importance"`
	Audience          []string `json:"audience,omitempty"`
	SortOrder         int      `json:"sort_order"`
	Notes             string   `json:"notes,omitempty"`
	ContentBytes      int      `json:"content_bytes,omitempty"`
}

type legacyGuidancePacket struct {
	ProjectID       string                 `json:"project_id"`
	ResolvedAt      string                 `json:"resolved_at"`
	Content         string                 `json:"content"`
	Sources         []legacyGuidanceSource `json:"sources"`
	SkippedSources  json.RawMessage        `json:"skipped_sources,omitempty"`
	ContentMarkdown string                 `json:"content_markdown,omitempty"`
	ContentSHA256   string                 `json:"content_sha256,omitempty"`
	ContentBytes    int                    `json:"content_bytes,omitempty"`
	Truncated       bool                   `json:"truncated,omitempty"`
	Incomplete      bool                   `json:"incomplete,omitempty"`
}

type legacyGuidanceSource struct {
	EntryID           int64    `json:"entry_id"`
	ID                int64    `json:"id"`
	ScopeProjectID    string   `json:"scope_project_id"`
	ProjectID         string   `json:"project_id"`
	DocumentProjectID string   `json:"document_project_id"`
	DocumentSlug      string   `json:"document_slug"`
	Slug              string   `json:"slug"`
	Title             string   `json:"title"`
	DocType           string   `json:"doc_type"`
	Tags              []string `json:"tags,omitempty"`
	UpdatedAt         string   `json:"updated_at"`
	Visibility        string   `json:"visibility,omitempty"`
	Importance        string   `json:"importance"`
	Audience          []string `json:"audience,omitempty"`
	SortOrder         int      `json:"sort_order"`
	Notes             string   `json:"notes,omitempty"`
	ContentBytes      int      `json:"content_bytes,omitempty"`
}

type guidanceEntriesResponse struct {
	Entries json.RawMessage `json:"entries"`
}

func guidanceCompatibleResponseBody(operation string, responseBody []byte) ([]byte, error) {
	switch operation {
	case "get_agent_guidance":
		return legacyGuidancePacketBody(responseBody)
	case "list_agent_guidance_entries":
		return legacyGuidanceEntriesBody(responseBody)
	default:
		return responseBody, nil
	}
}

func legacyGuidancePacketBody(responseBody []byte) ([]byte, error) {
	var packet guidancePacketResponse
	if err := json.Unmarshal(responseBody, &packet); err != nil {
		return nil, fmt.Errorf("decoding guidance packet response: %w", err)
	}
	legacySources := make([]legacyGuidanceSource, 0, len(packet.Sources))
	for _, source := range packet.Sources {
		legacySources = append(legacySources, legacyGuidanceSource{
			EntryID:           source.EntryID,
			ID:                source.EntryID,
			ScopeProjectID:    source.SourceScope,
			ProjectID:         source.SourceScope,
			DocumentProjectID: source.DocumentProjectID,
			DocumentSlug:      source.DocumentSlug,
			Slug:              source.DocumentSlug,
			Title:             source.DocumentTitle,
			DocType:           source.DocumentType,
			Tags:              source.Tags,
			UpdatedAt:         source.DocumentUpdatedAt,
			Visibility:        source.Visibility,
			Importance:        source.Importance,
			Audience:          source.Audience,
			SortOrder:         source.SortOrder,
			Notes:             source.Notes,
			ContentBytes:      source.ContentBytes,
		})
	}
	legacy := legacyGuidancePacket{
		ProjectID:       packet.ProjectID,
		ResolvedAt:      packet.ResolvedAt,
		Content:         packet.ContentMarkdown,
		Sources:         legacySources,
		SkippedSources:  compactRaw(packet.SkippedSources),
		ContentMarkdown: packet.ContentMarkdown,
		ContentSHA256:   packet.ContentSHA256,
		ContentBytes:    packet.ContentBytes,
		Truncated:       packet.Truncated,
		Incomplete:      packet.Incomplete,
	}
	return json.Marshal(legacy)
}

func legacyGuidanceEntriesBody(responseBody []byte) ([]byte, error) {
	var entries guidanceEntriesResponse
	if err := json.Unmarshal(responseBody, &entries); err != nil {
		return nil, fmt.Errorf("decoding guidance entries response: %w", err)
	}
	if len(bytes.TrimSpace(entries.Entries)) == 0 {
		return []byte("[]"), nil
	}
	return entries.Entries, nil
}

func buildGuidanceRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeGuidanceToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := guidanceRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := guidanceRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building guidance backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func decodeGuidanceToolArguments(raw json.RawMessage) (guidanceToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments guidanceToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return guidanceToolArguments{}, fmt.Errorf("decoding guidance tool arguments: %w", err)
	}
	return arguments, nil
}

func guidanceRESTRequestBody(operation string, arguments guidanceToolArguments) ([]byte, error) {
	switch operation {
	case "add_agent_guidance_entry":
		audience, err := parseStringList(arguments.Audience)
		if err != nil {
			return nil, err
		}
		return json.Marshal(guidanceEntryBody{
			DocumentProjectID: strings.TrimSpace(arguments.DocumentProjectID),
			DocumentSlug:      strings.TrimSpace(arguments.DocumentSlug),
			Importance:        strings.TrimSpace(arguments.Importance),
			Audience:          audience,
			SortOrder:         arguments.SortOrder,
			Notes:             strings.TrimSpace(arguments.Notes),
		})
	case "get_agent_guidance", "list_agent_guidance_entries", "delete_agent_guidance_entry":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: guidance operation %s", ErrUnsupportedAdapter, operation)
	}
}

func guidanceRESTURL(baseURL string, route Route, arguments guidanceToolArguments) (string, error) {
	routePath, err := expandGuidancePath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing guidance backend URL: %w", err)
	}
	query := parsedURL.Query()
	switch route.Operation {
	case "get_agent_guidance":
		query.Set("include_content", "true")
		if arguments.MaxBytes > 0 {
			query.Set("max_bytes", strconv.Itoa(arguments.MaxBytes))
		}
		if arguments.IncludeContent != nil {
			query.Set("include_content", strconv.FormatBool(*arguments.IncludeContent))
		}
	case "list_agent_guidance_entries":
		if arguments.IncludeGlobal {
			query.Set("include_global", "true")
		}
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandGuidancePath(path string, arguments guidanceToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("guidance route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{entry_id}") {
		if arguments.EntryID <= 0 {
			return "", fmt.Errorf("guidance route requires entry_id")
		}
		result = strings.ReplaceAll(result, "{entry_id}", strconv.FormatInt(arguments.EntryID, 10))
	}
	return result, nil
}
