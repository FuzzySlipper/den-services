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

type documentsToolArguments struct {
	ProjectID         string          `json:"project_id"`
	Slug              string          `json:"slug"`
	Title             string          `json:"title"`
	Content           string          `json:"content"`
	DocType           string          `json:"doc_type"`
	Tags              json.RawMessage `json:"tags"`
	Summary           string          `json:"summary"`
	Query             string          `json:"query"`
	Visibility        string          `json:"visibility"`
	ThreadID          int64           `json:"thread_id"`
	AuthorIdentity    string          `json:"author_identity"`
	BodyMarkdown      string          `json:"body_markdown"`
	ParentCommentID   *int64          `json:"parent_comment_id"`
	CommentKind       string          `json:"comment_kind"`
	Anchor            string          `json:"anchor"`
	Mentions          json.RawMessage `json:"mentions"`
	SourceRefs        json.RawMessage `json:"source_refs"`
	TargetType        string          `json:"target_type"`
	TargetProjectID   string          `json:"target_project_id"`
	TargetSlug        string          `json:"target_slug"`
	ThreadKey         string          `json:"thread_key"`
	CreatedBy         string          `json:"created_by"`
	Status            *string         `json:"status"`
	ResolutionSummary *string         `json:"resolution_summary"`
	IncludeComments   *bool           `json:"include_comments"`
	CreateIfMissing   *bool           `json:"create_if_missing"`
	IncludeResolved   *bool           `json:"include_resolved"`
	Limit             *int            `json:"limit"`
	Metadata          json.RawMessage `json:"metadata"`
}

type storeDocumentBody struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	DocType string   `json:"doc_type,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Summary string   `json:"summary,omitempty"`
}

type updateDocumentVisibilityBody struct {
	Visibility string `json:"visibility"`
}

type documentCommentBody struct {
	AuthorIdentity  string          `json:"author_identity"`
	BodyMarkdown    string          `json:"body_markdown"`
	ParentCommentID *int64          `json:"parent_comment_id,omitempty"`
	CommentKind     string          `json:"comment_kind,omitempty"`
	Anchor          string          `json:"anchor,omitempty"`
	Mentions        json.RawMessage `json:"mentions,omitempty"`
	SourceRefs      json.RawMessage `json:"source_refs,omitempty"`
}

type updateDiscussionThreadBody struct {
	Status            *string         `json:"status,omitempty"`
	Title             *string         `json:"title,omitempty"`
	Summary           *string         `json:"summary,omitempty"`
	ResolutionSummary *string         `json:"resolution_summary,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
}

func (c *Client) callDocumentsREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildDocumentsRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading documents backend response: %w", err)
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

func buildDocumentsRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeDocumentsToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := documentsRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := documentsRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building documents backend request: %w", err)
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

func decodeDocumentsToolArguments(raw json.RawMessage) (documentsToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments documentsToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return documentsToolArguments{}, fmt.Errorf("decoding documents tool arguments: %w", err)
	}
	return arguments, nil
}

func documentsRESTRequestBody(operation string, arguments documentsToolArguments) ([]byte, error) {
	switch operation {
	case "store_document":
		tags, err := parseStringList(arguments.Tags)
		if err != nil {
			return nil, err
		}
		return json.Marshal(storeDocumentBody{
			Slug:    strings.TrimSpace(arguments.Slug),
			Title:   strings.TrimSpace(arguments.Title),
			Content: arguments.Content,
			DocType: strings.TrimSpace(arguments.DocType),
			Tags:    tags,
			Summary: strings.TrimSpace(arguments.Summary),
		})
	case "update_document_visibility":
		return json.Marshal(updateDocumentVisibilityBody{Visibility: strings.TrimSpace(arguments.Visibility)})
	case "comment_on_document":
		return json.Marshal(documentCommentBody{
			AuthorIdentity:  strings.TrimSpace(arguments.AuthorIdentity),
			BodyMarkdown:    arguments.BodyMarkdown,
			ParentCommentID: arguments.ParentCommentID,
			CommentKind:     strings.TrimSpace(arguments.CommentKind),
			Anchor:          strings.TrimSpace(arguments.Anchor),
			Mentions:        compactRaw(arguments.Mentions),
			SourceRefs:      compactRaw(arguments.SourceRefs),
		})
	case "create_discussion_comment":
		return json.Marshal(documentCommentBody{
			AuthorIdentity:  strings.TrimSpace(arguments.AuthorIdentity),
			BodyMarkdown:    arguments.BodyMarkdown,
			ParentCommentID: arguments.ParentCommentID,
			CommentKind:     strings.TrimSpace(arguments.CommentKind),
			Mentions:        compactRaw(arguments.Mentions),
			SourceRefs:      compactRaw(arguments.SourceRefs),
		})
	case "update_discussion_thread":
		return json.Marshal(updateDiscussionThreadBody{
			Status:            arguments.Status,
			Title:             stringPtrFromValue(arguments.Title),
			Summary:           stringPtrFromValue(arguments.Summary),
			ResolutionSummary: arguments.ResolutionSummary,
			Metadata:          compactRaw(arguments.Metadata),
		})
	case "get_document", "list_documents", "search_documents", "delete_document", "archive_document_preflight", "query_archived_documents", "get_document_discussion", "list_discussion_threads", "get_discussion_thread":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: documents operation %s", ErrUnsupportedAdapter, operation)
	}
}

func documentsRESTURL(baseURL string, route Route, arguments documentsToolArguments) (string, error) {
	path := route.Path
	if strings.TrimSpace(arguments.ProjectID) != "" {
		switch route.Operation {
		case "list_documents":
			path = "/v1/projects/{project_id}/documents"
		case "search_documents":
			path = "/v1/projects/{project_id}/documents/search"
		case "query_archived_documents":
			if strings.TrimSpace(arguments.Query) != "" {
				path = "/v1/projects/{project_id}/documents/archived/search"
			} else {
				path = "/v1/projects/{project_id}/documents/archived"
			}
		}
	} else if route.Operation == "query_archived_documents" && strings.TrimSpace(arguments.Query) != "" {
		path = "/v1/documents/archived/search"
	}
	routePath, err := expandDocumentsPath(path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing documents backend URL: %w", err)
	}
	query := parsedURL.Query()
	switch route.Operation {
	case "list_documents":
		setStringValueQuery(query, "project_id", arguments.ProjectID)
		setStringValueQuery(query, "doc_type", arguments.DocType)
		setStringValueQuery(query, "tags", stringFromRaw(arguments.Tags))
		setStringValueQuery(query, "visibility", arguments.Visibility)
	case "search_documents":
		setStringValueQuery(query, "query", arguments.Query)
		setStringValueQuery(query, "project_id", arguments.ProjectID)
	case "query_archived_documents":
		setStringValueQuery(query, "query", arguments.Query)
		setStringValueQuery(query, "project_id", arguments.ProjectID)
		setStringValueQuery(query, "doc_type", arguments.DocType)
		setStringValueQuery(query, "tags", stringFromRaw(arguments.Tags))
	case "get_document_discussion":
		setBoolQuery(query, "create_if_missing", arguments.CreateIfMissing)
		setBoolQuery(query, "include_resolved", arguments.IncludeResolved)
		setStringValueQuery(query, "anchor", arguments.Anchor)
	case "list_discussion_threads":
		setStringValueQuery(query, "target_type", arguments.TargetType)
		setStringValueQuery(query, "target_project_id", arguments.TargetProjectID)
		setStringValueQuery(query, "target_slug", arguments.TargetSlug)
		if arguments.Status != nil {
			setStringQuery(query, "status", arguments.Status)
		}
		setIntQuery(query, "limit", arguments.Limit)
	case "get_discussion_thread":
		setBoolQuery(query, "include_comments", arguments.IncludeComments)
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandDocumentsPath(path string, arguments documentsToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("documents route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{slug}") {
		if strings.TrimSpace(arguments.Slug) == "" {
			return "", fmt.Errorf("documents route requires slug")
		}
		result = strings.ReplaceAll(result, "{slug}", url.PathEscape(strings.TrimSpace(arguments.Slug)))
	}
	if strings.Contains(result, "{thread_id}") {
		if arguments.ThreadID == 0 {
			return "", fmt.Errorf("documents route requires thread_id")
		}
		result = strings.ReplaceAll(result, "{thread_id}", strconv.FormatInt(arguments.ThreadID, 10))
	}
	return result, nil
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err == nil {
		if strings.TrimSpace(encoded) == "" {
			return nil
		}
		return json.RawMessage(encoded)
	}
	return trimmed
}

func stringPtrFromValue(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}
