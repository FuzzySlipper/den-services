package boardrelay

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
	"time"
)

type HTTPGitHubClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPGitHubClient(baseURL string, token string, timeout time.Duration) *HTTPGitHubClient {
	return &HTTPGitHubClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: timeout}}
}

func (c *HTTPGitHubClient) ListIssues(ctx context.Context, repository string) ([]GitHubIssue, error) {
	var all []GitHubIssue
	for page := 1; ; page++ {
		query := url.Values{"state": {"all"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var response []githubIssueDTO
		if err := c.getJSON(ctx, "/repos/"+repository+"/issues?"+query.Encode(), &response); err != nil {
			return nil, err
		}
		for _, issue := range response {
			if issue.PullRequest.URL != "" {
				continue
			}
			all = append(all, issue.toIssue())
		}
		if len(response) < 100 {
			return all, nil
		}
	}
}

func (c *HTTPGitHubClient) CreateIssue(ctx context.Context, repository string, title string, body string) (GitHubIssue, error) {
	var response githubIssueDTO
	if err := c.requestJSON(ctx, http.MethodPost, "/repos/"+repository+"/issues", struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}{title, body}, &response); err != nil {
		return GitHubIssue{}, err
	}
	return response.toIssue(), nil
}

func (c *HTTPGitHubClient) ListIssueComments(ctx context.Context, repository string, issueNumber int64) ([]GitHubComment, error) {
	var all []GitHubComment
	for page := 1; ; page++ {
		query := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		var response []githubCommentDTO
		if err := c.getJSON(ctx, "/repos/"+repository+"/issues/"+strconv.FormatInt(issueNumber, 10)+"/comments?"+query.Encode(), &response); err != nil {
			return nil, err
		}
		for _, comment := range response {
			all = append(all, comment.toComment())
		}
		if len(response) < 100 {
			return all, nil
		}
	}
}

func (c *HTTPGitHubClient) CreateIssueComment(ctx context.Context, repository string, issueNumber int64, body string) (GitHubComment, error) {
	var response githubCommentDTO
	if err := c.requestJSON(ctx, http.MethodPost, "/repos/"+repository+"/issues/"+strconv.FormatInt(issueNumber, 10)+"/comments", struct {
		Body string `json:"body"`
	}{body}, &response); err != nil {
		return GitHubComment{}, err
	}
	return response.toComment(), nil
}

func (c *HTTPGitHubClient) SetRepositoryVisibility(ctx context.Context, repository string, visibility string) error {
	private := visibility == "private"
	return c.requestJSON(ctx, http.MethodPatch, "/repos/"+repository, struct {
		Private bool `json:"private"`
	}{private}, &struct{}{})
}

func (c *HTTPGitHubClient) getJSON(ctx context.Context, path string, target any) error {
	return c.requestJSON(ctx, http.MethodGet, path, nil, target)
}

func (c *HTTPGitHubClient) requestJSON(ctx context.Context, method string, path string, source any, target any) error {
	var body io.Reader
	if source != nil {
		payload, err := json.Marshal(source)
		if err != nil {
			return fmt.Errorf("encoding github request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("building github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if source != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("calling github: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("github response status %d", response.StatusCode)
		}
		return fmt.Errorf("github response status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil && err != io.EOF {
		return fmt.Errorf("decoding github response: %w", err)
	}
	return nil
}

type githubUserDTO struct {
	Login string `json:"login"`
}
type githubPullRequestDTO struct {
	URL string `json:"url"`
}
type githubIssueDTO struct {
	ID          int64                `json:"id"`
	Number      int64                `json:"number"`
	Title       string               `json:"title"`
	Body        string               `json:"body"`
	HTMLURL     string               `json:"html_url"`
	User        githubUserDTO        `json:"user"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	PullRequest githubPullRequestDTO `json:"pull_request"`
}

func (d githubIssueDTO) toIssue() GitHubIssue {
	return GitHubIssue{ID: d.ID, Number: d.Number, Title: d.Title, Body: d.Body, Login: d.User.Login, HTMLURL: d.HTMLURL, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

type githubCommentDTO struct {
	ID        int64         `json:"id"`
	Body      string        `json:"body"`
	HTMLURL   string        `json:"html_url"`
	User      githubUserDTO `json:"user"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (d githubCommentDTO) toComment() GitHubComment {
	return GitHubComment{ID: d.ID, Body: d.Body, HTMLURL: d.HTMLURL, Login: d.User.Login, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}
