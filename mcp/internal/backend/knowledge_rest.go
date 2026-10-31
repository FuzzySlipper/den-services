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
	"time"

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
	Aliases           json.RawMessage `json:"aliases"`
	SourceRefs        json.RawMessage `json:"source_refs"`
	AccuracyNotes     *string         `json:"accuracy_notes"`
	ReplacementSlug   *string         `json:"replacement_slug"`
	LastReviewedAt    *string         `json:"last_reviewed_at"`
	ReviewDueAt       *string         `json:"review_due_at"`
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
	Slugs             []string        `json:"slugs"`
	Offset            *int            `json:"offset"`
	View              string          `json:"view"`
	Section           string          `json:"section"`
	KnownRevision     *int            `json:"known_revision"`
	KnownDigest       string          `json:"known_digest"`
	Links             json.RawMessage `json:"links"`
	MapEntries        json.RawMessage `json:"entries"`
}

// knowledgeStoreBody leaves optional metadata out when the caller omitted it,
// so the knowledge service keeps the entry's current value.
type knowledgeStoreBody struct {
	Slug            string                `json:"slug"`
	Title           string                `json:"title"`
	BodyMarkdown    string                `json:"body_markdown"`
	Kind            string                `json:"kind,omitempty"`
	Status          string                `json:"status,omitempty"`
	CurationState   string                `json:"curation_state,omitempty"`
	Summary         string                `json:"summary,omitempty"`
	Tags            []string              `json:"tags,omitempty"`
	Aliases         *[]string             `json:"aliases,omitempty"`
	SourceRefs      *[]knowledgeSourceRef `json:"source_refs,omitempty"`
	AccuracyNotes   *string               `json:"accuracy_notes,omitempty"`
	ReplacementSlug *string               `json:"replacement_slug,omitempty"`
	LastReviewedAt  *time.Time            `json:"last_reviewed_at,omitempty"`
	ReviewDueAt     *time.Time            `json:"review_due_at,omitempty"`
	ChangedBy       string                `json:"changed_by,omitempty"`
	ChangeNote      string                `json:"change_note,omitempty"`
}

type knowledgeSourceRef struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	ProjectID  string `json:"project_id,omitempty"`
	TaskID     *int64 `json:"task_id,omitempty"`
	MessageID  *int64 `json:"message_id,omitempty"`
	URL        string `json:"url,omitempty"`
	Note       string `json:"note,omitempty"`
}

type knowledgeSearchBody struct {
	Query             string   `json:"query"`
	RequiredTags      []string `json:"required_tags,omitempty"`
	AnyTags           []string `json:"any_tags,omitempty"`
	Kind              string   `json:"kind,omitempty"`
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
	ContextBudget     int      `json:"context_budget,omitempty"`
	IncludeFollowUps  *bool    `json:"include_follow_ups,omitempty"`
	IncludeDeprecated bool     `json:"include_deprecated,omitempty"`
	IncludeUnreviewed bool     `json:"include_unreviewed,omitempty"`
}

type knowledgeCardsBody struct {
	Slugs           []string `json:"slugs"`
	IncludeArchived bool     `json:"include_archived,omitempty"`
}

type knowledgeLinkBody struct {
	ToSlug      string `json:"to_slug"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

type knowledgeReplaceLinksBody struct {
	Links []knowledgeLinkBody `json:"links"`
}

type knowledgeMapEntryBody struct {
	EntrySlug string `json:"entry_slug"`
	GroupName string `json:"group_name,omitempty"`
	Position  int    `json:"position"`
	Note      string `json:"note,omitempty"`
}

type knowledgeStoreMapBody struct {
	Slug      string                  `json:"slug"`
	Title     string                  `json:"title"`
	Summary   string                  `json:"summary,omitempty"`
	Entries   []knowledgeMapEntryBody `json:"entries"`
	ChangedBy string                  `json:"changed_by,omitempty"`
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
	switch operation {
	case "den_knowledge_store":
		tags, err := parseStringList(arguments.Tags)
		if err != nil {
			return nil, err
		}
		body := knowledgeStoreBody{
			Slug: strings.TrimSpace(arguments.Slug), Title: strings.TrimSpace(arguments.Title), BodyMarkdown: arguments.BodyMarkdown,
			Kind: strings.TrimSpace(arguments.Kind), Status: strings.TrimSpace(arguments.Status), CurationState: strings.TrimSpace(arguments.CurationState),
			Summary: strings.TrimSpace(arguments.Summary), Tags: tags,
			AccuracyNotes: trimmedOptional(arguments.AccuracyNotes), ReplacementSlug: trimmedOptional(arguments.ReplacementSlug),
			ChangedBy: strings.TrimSpace(arguments.ChangedBy), ChangeNote: strings.TrimSpace(arguments.ChangeNote),
		}
		if body.Aliases, err = optionalStringList(arguments.Aliases); err != nil {
			return nil, err
		}
		if body.SourceRefs, err = optionalKnowledgeSourceRefs(arguments.SourceRefs); err != nil {
			return nil, err
		}
		if body.LastReviewedAt, err = optionalKnowledgeTime("last_reviewed_at", arguments.LastReviewedAt); err != nil {
			return nil, err
		}
		if body.ReviewDueAt, err = optionalKnowledgeTime("review_due_at", arguments.ReviewDueAt); err != nil {
			return nil, err
		}
		return json.Marshal(body)
	case "den_knowledge_search":
		return json.Marshal(knowledgeSearchBody{
			Query: strings.TrimSpace(arguments.Query), RequiredTags: requiredTags, AnyTags: anyTags, Kind: strings.TrimSpace(arguments.Kind),
			Status: strings.TrimSpace(arguments.Status), IncludeDeprecated: arguments.IncludeDeprecated,
			IncludeUnreviewed: arguments.IncludeUnreviewed, IncludeArchived: arguments.IncludeArchived, Limit: arguments.Limit,
		})
	case "den_knowledge_guide":
		contextBudget := 0
		if arguments.ContextBudget != nil {
			contextBudget = *arguments.ContextBudget
		}
		return json.Marshal(knowledgeGuideBody{
			Question: strings.TrimSpace(arguments.Question), RequiredTags: requiredTags, AnyTags: anyTags,
			ContextBudget: contextBudget, IncludeFollowUps: arguments.IncludeFollowUps, IncludeDeprecated: arguments.IncludeDeprecated,
			IncludeUnreviewed: arguments.IncludeUnreviewed,
		})
	case "den_knowledge_cards":
		slugs, err := normalizedKnowledgeSlugs(arguments.Slugs)
		if err != nil {
			return nil, err
		}
		return json.Marshal(knowledgeCardsBody{Slugs: slugs, IncludeArchived: arguments.IncludeArchived})
	case "den_knowledge_replace_links":
		links, err := decodeKnowledgeLinks(arguments.Links)
		if err != nil {
			return nil, err
		}
		return json.Marshal(knowledgeReplaceLinksBody{Links: links})
	case "den_knowledge_store_map":
		entries, err := decodeKnowledgeMapEntries(arguments.MapEntries)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(arguments.Slug) == "" || strings.TrimSpace(arguments.Title) == "" {
			return nil, fmt.Errorf("knowledge map requires slug and title")
		}
		return json.Marshal(knowledgeStoreMapBody{Slug: strings.TrimSpace(arguments.Slug), Title: strings.TrimSpace(arguments.Title), Summary: strings.TrimSpace(arguments.Summary), Entries: entries, ChangedBy: strings.TrimSpace(arguments.ChangedBy)})
	case "den_knowledge_get", "den_knowledge_delete", "den_knowledge_card", "den_knowledge_read", "den_knowledge_get_map":
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
	switch route.Operation {
	case "den_knowledge_get", "den_knowledge_card":
		if arguments.IncludeArchived {
			query.Set("include_archived", strconv.FormatBool(arguments.IncludeArchived))
		}
	case "den_knowledge_cards":
		if arguments.Offset != nil {
			if *arguments.Offset < 0 {
				return "", fmt.Errorf("knowledge cards offset must not be negative")
			}
			query.Set("offset", strconv.Itoa(*arguments.Offset))
		}
	case "den_knowledge_read":
		view := strings.TrimSpace(arguments.View)
		if view == "" {
			view = "outline"
		}
		if view != "outline" && view != "section" && view != "full" {
			return "", fmt.Errorf("knowledge read view must be outline, section, or full")
		}
		if view == "section" && strings.TrimSpace(arguments.Section) == "" {
			return "", fmt.Errorf("knowledge section read requires section")
		}
		query.Set("view", view)
		if strings.TrimSpace(arguments.Section) != "" {
			query.Set("section", strings.TrimSpace(arguments.Section))
		}
		if arguments.KnownRevision != nil {
			if *arguments.KnownRevision < 0 {
				return "", fmt.Errorf("knowledge known_revision must not be negative")
			}
			query.Set("known_revision", strconv.Itoa(*arguments.KnownRevision))
		}
		if strings.TrimSpace(arguments.KnownDigest) != "" {
			query.Set("known_digest", strings.TrimSpace(arguments.KnownDigest))
		}
		if arguments.IncludeArchived {
			query.Set("include_archived", strconv.FormatBool(arguments.IncludeArchived))
		}
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func normalizedKnowledgeSlugs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 100 {
		return nil, fmt.Errorf("knowledge cards require from 1 to 100 slugs")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("knowledge card slug is required")
		}
		result = append(result, value)
	}
	return result, nil
}

func trimmedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

// optionalStringList distinguishes an omitted list (nil) from an explicit
// empty one, which clears the stored value.
func optionalStringList(raw json.RawMessage) (*[]string, error) {
	if isOmittedJSON(raw) {
		return nil, nil
	}
	values, err := parseStringList(raw)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	return &values, nil
}

func optionalKnowledgeSourceRefs(raw json.RawMessage) (*[]knowledgeSourceRef, error) {
	if isOmittedJSON(raw) {
		return nil, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		raw = json.RawMessage(encoded)
	}
	refs := []knowledgeSourceRef{}
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("decoding knowledge source_refs: %w", err)
	}
	for index := range refs {
		refs[index].SourceKind = strings.TrimSpace(refs[index].SourceKind)
		refs[index].SourceID = strings.TrimSpace(refs[index].SourceID)
		if refs[index].SourceKind == "" || refs[index].SourceID == "" {
			return nil, fmt.Errorf("knowledge source_refs entries require source_kind and source_id")
		}
	}
	return &refs, nil
}

// optionalKnowledgeTime accepts an RFC 3339 timestamp or a YYYY-MM-DD date,
// which means midnight UTC.
func optionalKnowledgeTime(field string, value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	text := strings.TrimSpace(*value)
	for _, layout := range []string{time.RFC3339Nano, time.DateOnly} {
		if parsed, err := time.Parse(layout, text); err == nil {
			parsed = parsed.UTC()
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("knowledge %s must be an RFC 3339 timestamp or YYYY-MM-DD date", field)
}

func isOmittedJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func decodeKnowledgeLinks(raw json.RawMessage) ([]knowledgeLinkBody, error) {
	var links []knowledgeLinkBody
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, fmt.Errorf("decoding knowledge links: %w", err)
	}
	if len(links) > 20 {
		return nil, fmt.Errorf("knowledge links exceed maximum of 20")
	}
	for index := range links {
		links[index].ToSlug = strings.TrimSpace(links[index].ToSlug)
		links[index].Kind = strings.TrimSpace(links[index].Kind)
		links[index].Description = strings.TrimSpace(links[index].Description)
		if links[index].ToSlug == "" {
			return nil, fmt.Errorf("knowledge link target is required")
		}
		switch links[index].Kind {
		case "related", "embed", "replacement":
		default:
			return nil, fmt.Errorf("knowledge link kind must be related, embed, or replacement")
		}
	}
	return links, nil
}

func decodeKnowledgeMapEntries(raw json.RawMessage) ([]knowledgeMapEntryBody, error) {
	var entries []knowledgeMapEntryBody
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decoding knowledge map entries: %w", err)
	}
	if len(entries) == 0 || len(entries) > 100 {
		return nil, fmt.Errorf("knowledge map requires from 1 to 100 entries")
	}
	for index := range entries {
		entries[index].EntrySlug = strings.TrimSpace(entries[index].EntrySlug)
		entries[index].GroupName = strings.TrimSpace(entries[index].GroupName)
		entries[index].Note = strings.TrimSpace(entries[index].Note)
		if entries[index].EntrySlug == "" || entries[index].Position < 0 || entries[index].Position > 99 {
			return nil, fmt.Errorf("knowledge map entry has invalid slug or position")
		}
	}
	return entries, nil
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
