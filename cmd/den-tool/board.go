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
	"strconv"
	"strings"
)

const defaultBoardURL = "http://127.0.0.1:8100"

type BoardSearchOptions struct {
	ProjectID string
	Query     string
	AfterID   *int64
	Limit     *int
}

type BoardCreatePostOptions struct {
	ProjectID      string
	Title          string
	BodyMarkdown   string
	AuthorIdentity string
}

type BoardListPostsOptions struct {
	ProjectID string
	AfterID   *int64
	Limit     *int
}

type BoardCreateCommentOptions struct {
	PostID          int64
	ParentCommentID *int64
	BodyMarkdown    string
	AuthorIdentity  string
}

type BoardListCommentsOptions struct {
	PostID          int64
	ParentCommentID *int64
	AfterID         *int64
	Limit           *int
}

type BoardPurgeOptions struct {
	ID            int64
	ActorIdentity string
	Reason        string
}

type BoardClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

type BoardHTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *BoardHTTPError) Error() string {
	message := strings.TrimSpace(string(e.Body))
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	if message == "" {
		return fmt.Sprintf("board service returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("board service returned HTTP %d: %s", e.StatusCode, message)
}

func NewBoardClient(baseURL, serviceToken string, httpClient *http.Client) (*BoardClient, error) {
	normalizedBaseURL := strings.TrimSpace(baseURL)
	parsed, err := url.Parse(normalizedBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse board URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("board URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("board URL must include a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("board URL must not include a query or fragment")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &BoardClient{
		baseURL:      strings.TrimRight(normalizedBaseURL, "/"),
		serviceToken: serviceToken,
		httpClient:   httpClient,
	}, nil
}

func BoardClientFromEnv() (*BoardClient, error) {
	baseURL := strings.TrimSpace(os.Getenv("DEN_BOARD_URL"))
	if baseURL == "" {
		baseURL = defaultBoardURL
	}
	return NewBoardClient(baseURL, os.Getenv("DEN_BOARD_SERVICE_TOKEN"), http.DefaultClient)
}

func (c *BoardClient) Search(ctx context.Context, options BoardSearchOptions) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("board client is nil")
	}
	if strings.TrimSpace(options.ProjectID) == "" {
		return nil, fmt.Errorf("board project is required")
	}
	if strings.TrimSpace(options.Query) == "" {
		return nil, fmt.Errorf("board query is required")
	}
	if err := validatePage(options.AfterID, options.Limit); err != nil {
		return nil, err
	}

	requestURL, err := c.projectPostsURL(options.ProjectID, "search", options.AfterID, options.Limit, options.Query)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodGet, requestURL, nil)
}

func (c *BoardClient) CreatePost(ctx context.Context, options BoardCreatePostOptions) ([]byte, error) {
	if strings.TrimSpace(options.ProjectID) == "" || strings.TrimSpace(options.Title) == "" || strings.TrimSpace(options.BodyMarkdown) == "" || strings.TrimSpace(options.AuthorIdentity) == "" {
		return nil, fmt.Errorf("board create-post requires project, title, body, and author")
	}
	requestURL, err := c.projectPostsURL(options.ProjectID, "", nil, nil, "")
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, requestURL, struct {
		Title          string `json:"title"`
		BodyMarkdown   string `json:"body_markdown"`
		AuthorIdentity string `json:"author_identity"`
	}{options.Title, options.BodyMarkdown, options.AuthorIdentity})
}

func (c *BoardClient) ListPosts(ctx context.Context, options BoardListPostsOptions) ([]byte, error) {
	if strings.TrimSpace(options.ProjectID) == "" {
		return nil, fmt.Errorf("board list-posts requires project")
	}
	if err := validatePage(options.AfterID, options.Limit); err != nil {
		return nil, err
	}
	requestURL, err := c.projectPostsURL(options.ProjectID, "", options.AfterID, options.Limit, "")
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodGet, requestURL, nil)
}

func (c *BoardClient) GetPost(ctx context.Context, postID int64) ([]byte, error) {
	return c.get(ctx, "posts", postID, "", nil)
}

func (c *BoardClient) CreateComment(ctx context.Context, options BoardCreateCommentOptions) ([]byte, error) {
	if options.PostID <= 0 || strings.TrimSpace(options.BodyMarkdown) == "" || strings.TrimSpace(options.AuthorIdentity) == "" {
		return nil, fmt.Errorf("board create-comment requires positive post-id, body, and author")
	}
	if options.ParentCommentID != nil && *options.ParentCommentID <= 0 {
		return nil, fmt.Errorf("board parent-comment-id must be positive")
	}
	requestURL, err := c.resourceURL("posts", options.PostID, "comments", nil)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, requestURL, struct {
		ParentCommentID *int64 `json:"parent_comment_id,omitempty"`
		BodyMarkdown    string `json:"body_markdown"`
		AuthorIdentity  string `json:"author_identity"`
	}{options.ParentCommentID, options.BodyMarkdown, options.AuthorIdentity})
}

func (c *BoardClient) ListComments(ctx context.Context, options BoardListCommentsOptions) ([]byte, error) {
	if options.PostID <= 0 {
		return nil, fmt.Errorf("board list-comments requires positive post-id")
	}
	if options.ParentCommentID != nil && *options.ParentCommentID <= 0 {
		return nil, fmt.Errorf("board parent-comment-id must be positive")
	}
	if err := validatePage(options.AfterID, options.Limit); err != nil {
		return nil, err
	}
	query := url.Values{}
	setPageQuery(query, options.AfterID, options.Limit)
	if options.ParentCommentID != nil {
		query.Set("parent_comment_id", strconv.FormatInt(*options.ParentCommentID, 10))
	}
	requestURL, err := c.resourceURL("posts", options.PostID, "comments", query)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodGet, requestURL, nil)
}

func (c *BoardClient) GetComment(ctx context.Context, commentID int64) ([]byte, error) {
	return c.get(ctx, "comments", commentID, "", nil)
}

func (c *BoardClient) GetCommentPath(ctx context.Context, commentID int64, limit *int) ([]byte, error) {
	if err := validatePage(nil, limit); err != nil {
		return nil, err
	}
	query := url.Values{}
	if limit != nil {
		query.Set("limit", strconv.Itoa(*limit))
	}
	return c.get(ctx, "comments", commentID, "path", query)
}

func (c *BoardClient) PurgePost(ctx context.Context, options BoardPurgeOptions) ([]byte, error) {
	return c.purge(ctx, "posts", options)
}

func (c *BoardClient) PurgeComment(ctx context.Context, options BoardPurgeOptions) ([]byte, error) {
	return c.purge(ctx, "comments", options)
}

func (c *BoardClient) purge(ctx context.Context, resource string, options BoardPurgeOptions) ([]byte, error) {
	if options.ID <= 0 || strings.TrimSpace(options.ActorIdentity) == "" || strings.TrimSpace(options.Reason) == "" {
		return nil, fmt.Errorf("board purge requires positive id, actor, and reason")
	}
	requestURL, err := c.resourceURL(resource, options.ID, "", nil)
	if err != nil {
		return nil, err
	}
	body, err := c.request(ctx, http.MethodDelete, requestURL, struct {
		ActorIdentity string `json:"actor_identity"`
		Reason        string `json:"reason"`
	}{options.ActorIdentity, options.Reason})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return []byte(`{"purged":true}`), nil
	}
	return body, nil
}

func (c *BoardClient) get(ctx context.Context, resource string, id int64, suffix string, query url.Values) ([]byte, error) {
	if id <= 0 {
		return nil, fmt.Errorf("board id must be positive")
	}
	requestURL, err := c.resourceURL(resource, id, suffix, query)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodGet, requestURL, nil)
}

func (c *BoardClient) projectPostsURL(projectID, suffix string, afterID *int64, limit *int, searchQuery string) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse board URL: %w", err)
	}
	path := strings.TrimRight(parsed.Path, "/")
	escapedPath := strings.TrimRight(parsed.EscapedPath(), "/")
	segments := []string{"v1", "projects", projectID, "board", "posts"}
	if suffix != "" {
		segments = append(segments, suffix)
	}
	for _, segment := range segments {
		path += "/" + segment
		escapedPath += "/" + url.PathEscape(segment)
	}
	parsed.Path = path
	if escapedPath != path {
		parsed.RawPath = escapedPath
	} else {
		parsed.RawPath = ""
	}

	query := url.Values{}
	if searchQuery != "" {
		query.Set("q", searchQuery)
	}
	setPageQuery(query, afterID, limit)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c *BoardClient) resourceURL(resource string, id int64, suffix string, query url.Values) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse board URL: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/board/" + resource + "/" + strconv.FormatInt(id, 10)
	if suffix != "" {
		parsed.Path += "/" + suffix
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

func (c *BoardClient) request(ctx context.Context, method, requestURL string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode board request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("create board request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.serviceToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request board service: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read board response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &BoardHTTPError{StatusCode: response.StatusCode, Body: responseBody}
	}
	return responseBody, nil
}

func validatePage(afterID *int64, limit *int) error {
	if afterID != nil && *afterID < 0 {
		return fmt.Errorf("board after-id must be non-negative")
	}
	if limit != nil && (*limit <= 0 || *limit > 100) {
		return fmt.Errorf("board limit must be between 1 and 100")
	}
	return nil
}

func setPageQuery(query url.Values, afterID *int64, limit *int) {
	if afterID != nil {
		query.Set("after_id", strconv.FormatInt(*afterID, 10))
	}
	if limit != nil {
		query.Set("limit", strconv.Itoa(*limit))
	}
}
