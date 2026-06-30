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

type knowledgeToolArguments struct {
	Slug              string          `json:"slug"`
	Title             string          `json:"title"`
	BodyMarkdown      string          `json:"body_markdown"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	CurationState     string          `json:"curation_state"`
	Summary           string          `json:"summary"`
	Tags              json.RawMessage `json:"tags"`
	Audience          json.RawMessage `json:"audience"`
	Query             string          `json:"query"`
	Question          string          `json:"question"`
	RequiredTags      json.RawMessage `json:"required_tags"`
	AnyTags           json.RawMessage `json:"any_tags"`
	IncludeDeprecated bool            `json:"include_deprecated"`
	IncludeUnreviewed bool            `json:"include_unreviewed"`
	IncludeArchived   bool            `json:"include_archived"`
	IncludeFollowUps  *bool           `json:"include_follow_ups"`
	ContextBudget     *int            `json:"context_budget"`
	Limit             int             `json:"limit"`
	ChangedBy         string          `json:"changed_by"`
	ChangeNote        string          `json:"change_note"`
}

type knowledgeStoreBody struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	BodyMarkdown  string   `json:"body_markdown"`
	Kind          string   `json:"kind,omitempty"`
	Status        string   `json:"status,omitempty"`
	CurationState string   `json:"curation_state,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Audience      []string `json:"audience,omitempty"`
	ChangedBy     string   `json:"changed_by,omitempty"`
	ChangeNote    string   `json:"change_note,omitempty"`
}

type knowledgeSearchBody struct {
	Query             string   `json:"query"`
	RequiredTags      []string `json:"required_tags,omitempty"`
	AnyTags           []string `json:"any_tags,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	Status            string   `json:"status,omitempty"`
	IncludeDeprecated bool     `json:"include_deprecated,omitempty"`
	IncludeUnreviewed bool     `json:"include_unreviewed,omitempty"`
	IncludeArchived   bool     `json:"include_archived,omitempty"`
	Limit             int      `json:"limit,omitempty"`
}

type knowledgeGuideBody struct {
	Question          string   `json:"question"`
	RequiredTags      []string `json:"required_tags,omitempty"`
	AnyTags           []string `json:"any_tags,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	ContextBudget     int      `json:"context_budget,omitempty"`
	IncludeFollowUps  *bool    `json:"include_follow_ups,omitempty"`
	IncludeDeprecated bool     `json:"include_deprecated,omitempty"`
	IncludeUnreviewed bool     `json:"include_unreviewed,omitempty"`
}

func (c *Client) callKnowledgeREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildKnowledgeRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading knowledge backend response: %w", err)
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

func buildKnowledgeRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeKnowledgeToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := knowledgeRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := knowledgeRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building knowledge backend request: %w", err)
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

func decodeKnowledgeToolArguments(raw json.RawMessage) (knowledgeToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments knowledgeToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return knowledgeToolArguments{}, fmt.Errorf("decoding knowledge tool arguments: %w", err)
	}
	return arguments, nil
}

func knowledgeRESTRequestBody(operation string, arguments knowledgeToolArguments) ([]byte, error) {
	requiredTags, err := parseStringList(arguments.RequiredTags)
	if err != nil {
		return nil, err
	}
	anyTags, err := parseStringList(arguments.AnyTags)
	if err != nil {
		return nil, err
	}
	audience, err := parseStringList(arguments.Audience)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "den_knowledge_store":
		tags, err := parseStringList(arguments.Tags)
		if err != nil {
			return nil, err
		}
		return json.Marshal(knowledgeStoreBody{
			Slug: strings.TrimSpace(arguments.Slug), Title: strings.TrimSpace(arguments.Title), BodyMarkdown: arguments.BodyMarkdown,
			Kind: strings.TrimSpace(arguments.Kind), Status: strings.TrimSpace(arguments.Status), CurationState: strings.TrimSpace(arguments.CurationState),
			Summary: strings.TrimSpace(arguments.Summary), Tags: tags, Audience: audience,
			ChangedBy: strings.TrimSpace(arguments.ChangedBy), ChangeNote: strings.TrimSpace(arguments.ChangeNote),
		})
	case "den_knowledge_search":
		return json.Marshal(knowledgeSearchBody{
			Query: strings.TrimSpace(arguments.Query), RequiredTags: requiredTags, AnyTags: anyTags, Kind: strings.TrimSpace(arguments.Kind),
			Audience: audience, Status: strings.TrimSpace(arguments.Status), IncludeDeprecated: arguments.IncludeDeprecated,
			IncludeUnreviewed: arguments.IncludeUnreviewed, IncludeArchived: arguments.IncludeArchived, Limit: arguments.Limit,
		})
	case "den_knowledge_guide":
		contextBudget := 0
		if arguments.ContextBudget != nil {
			contextBudget = *arguments.ContextBudget
		}
		return json.Marshal(knowledgeGuideBody{
			Question: strings.TrimSpace(arguments.Question), RequiredTags: requiredTags, AnyTags: anyTags, Audience: audience,
			ContextBudget: contextBudget, IncludeFollowUps: arguments.IncludeFollowUps, IncludeDeprecated: arguments.IncludeDeprecated,
			IncludeUnreviewed: arguments.IncludeUnreviewed,
		})
	case "den_knowledge_get":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: knowledge operation %s", ErrUnsupportedAdapter, operation)
	}
}

func knowledgeRESTURL(baseURL string, route Route, arguments knowledgeToolArguments) (string, error) {
	routePath, err := expandKnowledgePath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing knowledge backend URL: %w", err)
	}
	query := parsedURL.Query()
	if route.Operation == "den_knowledge_get" && arguments.IncludeArchived {
		query.Set("include_archived", strconv.FormatBool(arguments.IncludeArchived))
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandKnowledgePath(path string, arguments knowledgeToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{slug}") {
		if strings.TrimSpace(arguments.Slug) == "" {
			return "", fmt.Errorf("knowledge route requires slug")
		}
		result = strings.ReplaceAll(result, "{slug}", url.PathEscape(strings.TrimSpace(arguments.Slug)))
	}
	return result, nil
}
