package boardrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPBoardClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPBoardClient(baseURL string, token string, timeout time.Duration) *HTTPBoardClient {
	return &HTTPBoardClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: timeout}}
}

func (c *HTTPBoardClient) ListPosts(ctx context.Context, projectID string, afterID *int64, limit int) (BoardPage, error) {
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if afterID != nil {
		query.Set("after_id", strconv.FormatInt(*afterID, 10))
	}
	var response struct {
		Posts       []BoardPost `json:"posts"`
		NextAfterID *int64      `json:"next_after_id"`
	}
	err := c.getJSON(ctx, "/v1/projects/"+url.PathEscape(projectID)+"/board/posts?"+query.Encode(), &response)
	return BoardPage{Posts: response.Posts, NextAfterID: response.NextAfterID}, err
}

func (c *HTTPBoardClient) GetPost(ctx context.Context, postID int64) (*BoardPost, error) {
	var post BoardPost
	if err := c.getJSON(ctx, "/v1/board/posts/"+strconv.FormatInt(postID, 10), &post); err != nil {
		return nil, err
	}
	return &post, nil
}

func (c *HTTPBoardClient) GetComment(ctx context.Context, commentID int64) (*BoardComment, error) {
	var comment BoardComment
	if err := c.getJSON(ctx, "/v1/board/comments/"+strconv.FormatInt(commentID, 10), &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (c *HTTPBoardClient) ListComments(ctx context.Context, postID int64, parentCommentID *int64, afterID *int64, limit int) (CommentPage, error) {
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if parentCommentID != nil {
		query.Set("parent_comment_id", strconv.FormatInt(*parentCommentID, 10))
	}
	if afterID != nil {
		query.Set("after_id", strconv.FormatInt(*afterID, 10))
	}
	var response struct {
		Comments    []BoardComment `json:"comments"`
		NextAfterID *int64         `json:"next_after_id"`
	}
	err := c.getJSON(ctx, "/v1/board/posts/"+strconv.FormatInt(postID, 10)+"/comments?"+query.Encode(), &response)
	return CommentPage{Comments: response.Comments, NextAfterID: response.NextAfterID}, err
}

func (c *HTTPBoardClient) CreatePost(ctx context.Context, projectID string, request BoardCreatePostRequest) (*BoardPost, error) {
	var post BoardPost
	err := c.requestJSON(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(projectID)+"/board/posts", request, &post)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (c *HTTPBoardClient) CreateComment(ctx context.Context, postID int64, request BoardCreateCommentRequest) (*BoardComment, error) {
	var comment BoardComment
	err := c.requestJSON(ctx, http.MethodPost, "/v1/board/posts/"+strconv.FormatInt(postID, 10)+"/comments", request, &comment)
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (c *HTTPBoardClient) getJSON(ctx context.Context, path string, target any) error {
	return c.requestJSON(ctx, http.MethodGet, path, nil, target)
}

func (c *HTTPBoardClient) requestJSON(ctx context.Context, method string, path string, source any, target any) error {
	var body *bytes.Reader
	if source == nil {
		body = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(source)
		if err != nil {
			return fmt.Errorf("encoding board request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("building board request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if source != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("calling board: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("board response status %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding board response: %w", err)
	}
	return nil
}
