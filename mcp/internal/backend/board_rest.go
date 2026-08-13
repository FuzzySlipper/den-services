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

type boardToolArguments struct {
	ProjectID       string `json:"project_id"`
	PostID          int64  `json:"post_id"`
	CommentID       int64  `json:"comment_id"`
	ParentCommentID *int64 `json:"parent_comment_id"`
	AfterID         *int64 `json:"after_id"`
	Limit           *int   `json:"limit"`
	Title           string `json:"title"`
	BodyMarkdown    string `json:"body_markdown"`
	AuthorIdentity  string `json:"author_identity"`
	ActorIdentity   string `json:"actor_identity"`
	Reason          string `json:"reason"`
}

type boardPostBody struct {
	Title          string `json:"title"`
	BodyMarkdown   string `json:"body_markdown"`
	AuthorIdentity string `json:"author_identity"`
}

func (c *Client) callBoardREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildBoardRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading board backend response: %w", err)
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

func buildBoardRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	var arguments boardToolArguments
	raw := call.Arguments
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return nil, fmt.Errorf("decoding board tool arguments: %w", err)
	}

	path, err := expandBoardPath(route.Path, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := url.Parse(backend.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parsing board backend URL: %w", err)
	}
	query := requestURL.Query()
	if arguments.ParentCommentID != nil {
		query.Set("parent_comment_id", strconv.FormatInt(*arguments.ParentCommentID, 10))
	}
	if arguments.AfterID != nil {
		query.Set("after_id", strconv.FormatInt(*arguments.AfterID, 10))
	}
	if arguments.Limit != nil {
		query.Set("limit", strconv.Itoa(*arguments.Limit))
	}
	requestURL.RawQuery = query.Encode()

	var body any
	switch route.Operation {
	case "create_board_post":
		body = boardPostBody{arguments.Title, arguments.BodyMarkdown, arguments.AuthorIdentity}
	case "create_board_comment":
		body = struct {
			ParentCommentID *int64 `json:"parent_comment_id,omitempty"`
			BodyMarkdown    string `json:"body_markdown"`
			AuthorIdentity  string `json:"author_identity"`
		}{arguments.ParentCommentID, arguments.BodyMarkdown, arguments.AuthorIdentity}
	case "purge_board_post", "purge_board_comment":
		body = struct {
			ActorIdentity string `json:"actor_identity"`
			Reason        string `json:"reason"`
		}{arguments.ActorIdentity, arguments.Reason}
	case "list_board_posts", "get_board_post", "list_board_comments", "get_board_comment", "get_board_comment_path":
	default:
		return nil, fmt.Errorf("%w: board operation %s", ErrUnsupportedAdapter, route.Operation)
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding board request: %w", err)
		}
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building board backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func expandBoardPath(path string, arguments boardToolArguments) (string, error) {
	if strings.Contains(path, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("board route requires project_id")
		}
		path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(path, "{post_id}") {
		if arguments.PostID <= 0 {
			return "", fmt.Errorf("board route requires post_id")
		}
		path = strings.ReplaceAll(path, "{post_id}", strconv.FormatInt(arguments.PostID, 10))
	}
	if strings.Contains(path, "{comment_id}") {
		if arguments.CommentID <= 0 {
			return "", fmt.Errorf("board route requires comment_id")
		}
		path = strings.ReplaceAll(path, "{comment_id}", strconv.FormatInt(arguments.CommentID, 10))
	}
	return path, nil
}
