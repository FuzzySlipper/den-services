package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"den-services/mcp/internal/config"
)

const (
	assignmentManifestSchemaVersion  = "1"
	assignmentManifestDefaultHandles = 12
	assignmentManifestMaxHandles     = 24
	assignmentManifestDefaultInline  = 1024
	assignmentManifestMaxInline      = 4096
	assignmentManifestDefaultLibrary = 4
	assignmentManifestMaxLibrary     = 8
)

type assignmentManifestArguments struct {
	TaskID                int64                        `json:"task_id"`
	Assignment            string                       `json:"assignment"`
	Background            string                       `json:"background"`
	AgentProfile          string                       `json:"agent_profile"`
	Capabilities          []string                     `json:"capabilities"`
	ExplicitKnowledgeRefs []string                     `json:"explicit_knowledge_refs"`
	InheritedRefs         []assignmentManifestRef      `json:"inherited_refs"`
	Limits                assignmentManifestInputLimit `json:"limits"`
}

type assignmentManifestInputLimit struct {
	MaxHandles     *int `json:"max_handles"`
	InlineBudget   *int `json:"inline_budget"`
	LibrarianItems *int `json:"librarian_items"`
}

type assignmentManifestLimits struct {
	MaxHandles     int `json:"max_handles"`
	InlineBudget   int `json:"inline_budget"`
	LibrarianItems int `json:"librarian_items"`
}

// assignmentManifestRef is intentionally a card, never a source body. It is
// also the portable inherited-manifest shape used by orchestrators.
type assignmentManifestRef struct {
	Reference       string   `json:"reference"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary,omitempty"`
	Kind            string   `json:"kind"`
	Authority       string   `json:"authority"`
	AuthorityState  string   `json:"authority_state"`
	CurationState   string   `json:"curation_state,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Revision        *int     `json:"revision,omitempty"`
	UpdateMarker    string   `json:"update_marker,omitempty"`
	ReadCostBytes   int      `json:"read_cost_bytes,omitempty"`
	SelectionSource string   `json:"selection_source"`
	ReadWhen        string   `json:"read_when,omitempty"`
	ReadPolicy      string   `json:"read_policy"`
	Group           string   `json:"group"`
}

type assignmentManifestResponse struct {
	SchemaVersion string                    `json:"schema_version"`
	GeneratedAt   string                    `json:"generated_at"`
	ProjectID     string                    `json:"project_id"`
	TaskID        int64                     `json:"task_id"`
	Assignment    string                    `json:"assignment"`
	Background    string                    `json:"background,omitempty"`
	AgentProfile  string                    `json:"agent_profile,omitempty"`
	Capabilities  []string                  `json:"capabilities,omitempty"`
	References    []assignmentManifestRef   `json:"references"`
	Markdown      string                    `json:"markdown"`
	Limits        assignmentManifestLimits  `json:"limits"`
	Truncated     bool                      `json:"truncated"`
	SourceStatus  []taskContextSourceStatus `json:"source_status"`
}

type assignmentContextResolution struct {
	Selections []assignmentBindingSelection `json:"selections"`
	Truncated  bool                         `json:"truncated"`
}

type assignmentBindingSelection struct {
	TargetKind      string `json:"target_kind"`
	TargetRef       string `json:"target_ref"`
	ScopeKind       string `json:"scope_kind"`
	ScopeRef        string `json:"scope_ref"`
	Priority        int    `json:"priority"`
	SortOrder       int    `json:"sort_order"`
	ReadPolicy      string `json:"read_policy"`
	ReadWhen        string `json:"read_when"`
	SelectionSource string `json:"selection_source"`
	Target          *struct {
		Slug          string   `json:"slug"`
		Title         string   `json:"title"`
		Summary       string   `json:"summary"`
		Kind          string   `json:"kind"`
		Status        string   `json:"status"`
		CurationState string   `json:"curation_state"`
		Tags          []string `json:"tags"`
		Revision      int      `json:"revision"`
		RevisionKnown bool     `json:"revision_known"`
		UpdatedAt     string   `json:"updated_at"`
	} `json:"target"`
}

type assignmentKnowledgeMetadata struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	CurationState string   `json:"curation_state"`
	Tags          []string `json:"tags"`
	Revision      int      `json:"revision"`
	RevisionKnown bool     `json:"revision_known"`
	UpdatedAt     string   `json:"updated_at"`
}

type assignmentManifestCandidate struct {
	ref         assignmentManifestRef
	priority    int
	sort        int
	specificity int
}

func (c *Client) callAssignmentManifestCompose(ctx context.Context, backends map[string]config.BackendConfig, _ Route, call ToolCall) (Result, *Failure, error) {
	arguments, err := decodeAssignmentManifestArguments(call.Arguments)
	if err != nil {
		return Result{}, nil, err
	}
	tasksBackend, ok := backends[taskWorkflowTasksBackend]
	if !ok {
		return Result{}, nil, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowTasksBackend)
	}
	taskHandle := "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10)
	taskBody, failure, err := c.taskContextGET(ctx, tasksBackend, taskHandle, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var detail taskContextTaskDetail
	if err := json.Unmarshal(taskBody, &detail); err != nil {
		return Result{}, nil, fmt.Errorf("parsing assignment manifest canonical task: %w", err)
	}
	projectID := strings.TrimSpace(detail.Task.ProjectID)
	if detail.Task.ID != arguments.TaskID || projectID == "" {
		return Result{}, nil, fmt.Errorf("assignment manifest canonical task missing task identity")
	}

	limits, err := normalizeAssignmentManifestLimits(arguments.Limits)
	if err != nil {
		return Result{}, nil, err
	}
	response := assignmentManifestResponse{
		SchemaVersion: assignmentManifestSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ProjectID: projectID, TaskID: arguments.TaskID, Assignment: arguments.Assignment, Background: arguments.Background,
		AgentProfile: arguments.AgentProfile, Capabilities: trimStrings(arguments.Capabilities), References: []assignmentManifestRef{}, Limits: limits,
		SourceStatus: []taskContextSourceStatus{{Source: "task", State: "ok", Handle: taskHandle, Retryable: false}},
	}
	candidates := make([]assignmentManifestCandidate, 0, len(arguments.InheritedRefs)+len(arguments.ExplicitKnowledgeRefs)+limits.MaxHandles)
	for _, inherited := range arguments.InheritedRefs {
		if candidate, ok := normalizedInheritedCandidate(inherited); ok {
			candidates = append(candidates, candidate)
		}
	}

	if guidanceBackend, exists := backends["guidance"]; exists {
		handle := "/v1/guidance/context-resolve"
		body, downstreamFailure, downstreamErr := c.taskContextPOST(ctx, guidanceBackend, handle, assignmentContextResolveBody(projectID, arguments, limits), call)
		if downstreamErr != nil || downstreamFailure != nil {
			response.SourceStatus = append(response.SourceStatus, taskContextStatus("guidance", handle, downstreamFailure, downstreamErr))
		} else {
			var resolution assignmentContextResolution
			if err := json.Unmarshal(body, &resolution); err != nil {
				response.SourceStatus = append(response.SourceStatus, taskContextStatus("guidance", handle, nil, err))
			} else {
				for _, selection := range resolution.Selections {
					candidate, ok := assignmentBindingCandidate(selection)
					if ok && assignmentRelevant(candidate.ref, arguments.Assignment, arguments.Background) {
						candidates = append(candidates, candidate)
					}
				}
				state := "ok"
				if resolution.Truncated {
					state = "partial"
					response.Truncated = true
				}
				response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "guidance", State: state, Handle: handle, Retryable: false})
			}
		}
	} else {
		response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "guidance", State: "unavailable", Handle: "guidance", ErrorCode: "den_backend_config_error", Retryable: false})
	}

	if knowledgeBackend, exists := backends["knowledge"]; exists {
		for _, slug := range sortedUniqueStrings(arguments.ExplicitKnowledgeRefs) {
			candidate, status := c.assignmentExplicitKnowledge(ctx, knowledgeBackend, slug, call)
			if status != nil {
				response.SourceStatus = append(response.SourceStatus, *status)
				continue
			}
			candidates = append(candidates, candidate)
		}
	} else if len(arguments.ExplicitKnowledgeRefs) > 0 {
		response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "knowledge", State: "unavailable", Handle: "knowledge", ErrorCode: "den_backend_config_error", Retryable: false})
		for _, slug := range sortedUniqueStrings(arguments.ExplicitKnowledgeRefs) {
			candidates = append(candidates, explicitPlaceholderCandidate(slug))
		}
	}

	if limits.LibrarianItems > 0 {
		if librarianBackend, exists := backends["librarian"]; exists {
			handle := "/v1/projects/" + url.PathEscape(projectID) + "/librarian/query"
			body, downstreamFailure, downstreamErr := c.taskContextPOST(ctx, librarianBackend, handle, librarianQueryBody{Query: strings.TrimSpace(arguments.Assignment + " " + arguments.Background), TaskID: &arguments.TaskID, IncludeGlobal: boolPointer(true), SourceLimits: librarianManifestLimits(limits.LibrarianItems)}, call)
			if downstreamErr != nil || downstreamFailure != nil {
				response.SourceStatus = append(response.SourceStatus, taskContextStatus("librarian", handle, downstreamFailure, downstreamErr))
			} else {
				var wire taskContextLibrarianWire
				if err := json.Unmarshal(body, &wire); err != nil {
					response.SourceStatus = append(response.SourceStatus, taskContextStatus("librarian", handle, nil, err))
				} else {
					items := wire.RelevantItems
					if len(items) > limits.LibrarianItems {
						items = items[:limits.LibrarianItems]
						response.Truncated = true
					}
					for _, item := range items {
						if candidate, ok := librarianManifestCandidate(item); ok {
							candidates = append(candidates, candidate)
						}
					}
					state, errorCode := "ok", ""
					if len(wire.Warnings) > 0 {
						state, errorCode = "partial", "librarian_warnings"
					}
					response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "librarian", State: state, Handle: handle, ErrorCode: errorCode, Retryable: false})
				}
			}
		} else {
			response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "librarian", State: "unavailable", Handle: "librarian", ErrorCode: "den_backend_config_error", Retryable: false})
		}
	}

	response.References = chooseManifestReferences(candidates, limits.MaxHandles)
	response.Truncated = response.Truncated || len(deduplicateManifestCandidates(candidates)) > len(response.References)
	response.Markdown = renderAssignmentManifestMarkdown(response)
	sort.Slice(response.SourceStatus, func(i, j int) bool { return response.SourceStatus[i].Source < response.SourceStatus[j].Source })
	body, err := json.Marshal(response)
	if err != nil {
		return Result{}, nil, fmt.Errorf("encoding assignment manifest: %w", err)
	}
	if len(body) > 128*1024 {
		return Result{}, nil, fmt.Errorf("assignment manifest response exceeds byte bound")
	}
	result, err := buildRESTToolResult(body)
	if err != nil {
		return Result{}, nil, err
	}
	if len(result) > 128*1024 {
		return Result{}, nil, fmt.Errorf("assignment manifest MCP result exceeds byte bound")
	}
	return Result{Value: result}, nil, nil
}

func decodeAssignmentManifestArguments(raw json.RawMessage) (assignmentManifestArguments, error) {
	var arguments assignmentManifestArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return arguments, fmt.Errorf("decoding assignment manifest arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return arguments, fmt.Errorf("decoding assignment manifest arguments: unexpected trailing JSON")
	}
	arguments.Assignment, arguments.Background, arguments.AgentProfile = strings.TrimSpace(arguments.Assignment), strings.TrimSpace(arguments.Background), strings.TrimSpace(arguments.AgentProfile)
	if arguments.TaskID <= 0 {
		return arguments, fmt.Errorf("assignment manifest route requires task_id")
	}
	if arguments.Assignment == "" {
		return arguments, fmt.Errorf("assignment manifest route requires non-empty assignment")
	}
	if len(raw) > 64*1024 || len(arguments.Assignment) > 16*1024 || len(arguments.Background) > 16*1024 {
		return arguments, fmt.Errorf("assignment manifest input exceeds byte bounds")
	}
	if len(arguments.Capabilities) > 32 || len(arguments.ExplicitKnowledgeRefs) > 100 || len(arguments.InheritedRefs) > 100 {
		return arguments, fmt.Errorf("assignment manifest input collection exceeds bounds")
	}
	return arguments, nil
}

func normalizeAssignmentManifestLimits(input assignmentManifestInputLimit) (assignmentManifestLimits, error) {
	result := assignmentManifestLimits{MaxHandles: assignmentManifestDefaultHandles, InlineBudget: assignmentManifestDefaultInline, LibrarianItems: assignmentManifestDefaultLibrary}
	if input.MaxHandles != nil {
		result.MaxHandles = *input.MaxHandles
	}
	if input.InlineBudget != nil {
		result.InlineBudget = *input.InlineBudget
	}
	if input.LibrarianItems != nil {
		result.LibrarianItems = *input.LibrarianItems
	}
	if result.MaxHandles < 1 || result.MaxHandles > assignmentManifestMaxHandles {
		return result, fmt.Errorf("assignment manifest max_handles must be from 1 to %d", assignmentManifestMaxHandles)
	}
	if result.InlineBudget < 0 || result.InlineBudget > assignmentManifestMaxInline {
		return result, fmt.Errorf("assignment manifest inline_budget must be from 0 to %d", assignmentManifestMaxInline)
	}
	if result.LibrarianItems < 0 || result.LibrarianItems > assignmentManifestMaxLibrary {
		return result, fmt.Errorf("assignment manifest librarian_items must be from 0 to %d", assignmentManifestMaxLibrary)
	}
	return result, nil
}

func assignmentContextResolveBody(projectID string, arguments assignmentManifestArguments, limits assignmentManifestLimits) any {
	scopes := []map[string]string{{"kind": "task", "ref": strconv.FormatInt(arguments.TaskID, 10)}}
	if arguments.AgentProfile != "" {
		scopes = append(scopes, map[string]string{"kind": "agent_profile", "ref": arguments.AgentProfile})
	}
	for _, capability := range trimStrings(arguments.Capabilities) {
		scopes = append(scopes, map[string]string{"kind": "capability", "ref": capability})
	}
	return struct {
		ProjectID    string              `json:"project_id"`
		Scopes       []map[string]string `json:"scopes"`
		InlineBudget int                 `json:"inline_budget"`
	}{projectID, scopes, limits.InlineBudget}
}

func (c *Client) assignmentExplicitKnowledge(ctx context.Context, backend config.BackendConfig, slug string, call ToolCall) (assignmentManifestCandidate, *taskContextSourceStatus) {
	handle := "/v1/knowledge/entries/" + url.PathEscape(slug) + "/card"
	body, failure, err := c.taskContextGET(ctx, backend, handle, call)
	if err != nil || failure != nil {
		status := taskContextStatus("knowledge", handle, failure, err)
		return assignmentManifestCandidate{}, &status
	}
	var metadata assignmentKnowledgeMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		status := taskContextStatus("knowledge", handle, nil, err)
		return assignmentManifestCandidate{}, &status
	}
	if strings.TrimSpace(metadata.Slug) == "" {
		metadata.Slug = slug
	}
	return knowledgeManifestCandidate(metadata, "explicit", "on_demand", "Explicit orchestrator selection."), nil
}

func explicitPlaceholderCandidate(slug string) assignmentManifestCandidate {
	return assignmentManifestCandidate{ref: assignmentManifestRef{Reference: "knowledge:" + slug, Title: slug, Kind: "knowledge", Authority: "knowledge", AuthorityState: "unavailable", SelectionSource: "explicit", ReadPolicy: "on_demand", ReadWhen: "Explicit orchestrator selection."}, priority: 100}
}

func knowledgeManifestCandidate(metadata assignmentKnowledgeMetadata, source, policy, readWhen string) assignmentManifestCandidate {
	ref := assignmentManifestRef{Reference: "knowledge:" + metadata.Slug, Title: boundedManifestText(metadata.Title, 512), Summary: boundedManifestText(metadata.Summary, 2048), Kind: metadata.Kind, Authority: "knowledge", CurationState: metadata.CurationState, Tags: trimStrings(metadata.Tags), SelectionSource: source, ReadPolicy: policy, ReadWhen: boundedManifestText(readWhen, 1024), ReadCostBytes: len(metadata.Summary)}
	ref.AuthorityState = strings.TrimSpace(metadata.Status)
	if ref.AuthorityState == "" {
		ref.AuthorityState = "active"
	}
	if ref.Title == "" {
		ref.Title = metadata.Slug
	}
	if ref.Kind == "" {
		ref.Kind = "knowledge"
	}
	if metadata.RevisionKnown || metadata.Revision > 0 {
		value := metadata.Revision
		ref.Revision = &value
	} else {
		ref.UpdateMarker = metadata.UpdatedAt
	}
	return assignmentManifestCandidate{ref: ref}
}

func assignmentBindingCandidate(selection assignmentBindingSelection) (assignmentManifestCandidate, bool) {
	if selection.TargetKind == "knowledge" && selection.Target != nil {
		metadata := assignmentKnowledgeMetadata{Slug: selection.Target.Slug, Title: selection.Target.Title, Summary: selection.Target.Summary, Kind: selection.Target.Kind, Status: selection.Target.Status, CurationState: selection.Target.CurationState, Tags: selection.Target.Tags, Revision: selection.Target.Revision, RevisionKnown: selection.Target.RevisionKnown, UpdatedAt: selection.Target.UpdatedAt}
		candidate := knowledgeManifestCandidate(metadata, selection.SelectionSource, selection.ReadPolicy, selection.ReadWhen)
		candidate.priority, candidate.sort, candidate.specificity = selection.Priority, selection.SortOrder, manifestScopeSpecificity(selection.ScopeKind)
		return candidate, true
	}
	if selection.TargetKind != "document" || strings.TrimSpace(selection.TargetRef) == "" {
		return assignmentManifestCandidate{}, false
	}
	return assignmentManifestCandidate{ref: assignmentManifestRef{Reference: "document:" + selection.TargetRef, Title: selection.TargetRef, Kind: "document", Authority: "guidance", AuthorityState: "active", SelectionSource: selection.SelectionSource, ReadPolicy: selection.ReadPolicy, ReadWhen: selection.ReadWhen}, priority: selection.Priority, sort: selection.SortOrder, specificity: manifestScopeSpecificity(selection.ScopeKind)}, true
}

func normalizedInheritedCandidate(ref assignmentManifestRef) (assignmentManifestCandidate, bool) {
	ref.Reference, ref.Title, ref.Kind = strings.TrimSpace(ref.Reference), strings.TrimSpace(ref.Title), strings.TrimSpace(ref.Kind)
	ref.Summary, ref.UpdateMarker, ref.ReadWhen = strings.TrimSpace(ref.Summary), strings.TrimSpace(ref.UpdateMarker), strings.TrimSpace(ref.ReadWhen)
	if ref.Reference == "" || ref.Summary == "" || (ref.Revision == nil && ref.UpdateMarker == "") || ref.ReadWhen == "" {
		return assignmentManifestCandidate{}, false
	}
	if ref.Title == "" {
		ref.Title = ref.Reference
	}
	if ref.Kind == "" {
		ref.Kind = "reference"
	}
	if ref.Authority == "" {
		ref.Authority = "inherited_manifest"
	}
	if ref.AuthorityState == "" {
		ref.AuthorityState = "inherited"
	}
	ref.SelectionSource = "inherited_manifest"
	ref.Title = boundedManifestText(ref.Title, 512)
	ref.Summary = boundedManifestText(ref.Summary, 2048)
	ref.ReadWhen = boundedManifestText(ref.ReadWhen, 1024)
	if ref.ReadCostBytes == 0 {
		ref.ReadCostBytes = len(ref.Summary)
	}
	if ref.ReadPolicy == "" {
		ref.ReadPolicy = "on_demand"
	}
	if manifestGroupRank(ref.Group) == 4 {
		ref.Group = ""
	}
	return assignmentManifestCandidate{ref: ref, priority: 90}, true
}

func librarianManifestLimits(limit int) json.RawMessage {
	value, _ := json.Marshal(map[string]int{"knowledge": limit, "documents": limit})
	return value
}

func librarianManifestCandidate(raw json.RawMessage) (assignmentManifestCandidate, bool) {
	var item struct {
		Type        string   `json:"type"`
		Source      string   `json:"source"`
		SourceID    string   `json:"source_id"`
		ProjectID   string   `json:"project_id"`
		Title       string   `json:"title"`
		Summary     string   `json:"summary"`
		Tags        []string `json:"tags"`
		Kind        string   `json:"kind"`
		UpdatedAt   string   `json:"updated_at"`
		Revision    int      `json:"revision"`
		WhyRelevant string   `json:"why_relevant"`
	}
	if json.Unmarshal(raw, &item) != nil || strings.TrimSpace(item.SourceID) == "" {
		return assignmentManifestCandidate{}, false
	}
	if item.Type == "" {
		item.Type = item.Source
	}
	item.Type = strings.TrimSpace(item.Type)
	reference, authority := item.Type+":"+item.SourceID, "librarian"
	if item.Type == "knowledge" {
		reference, authority = "knowledge:"+item.SourceID, "knowledge"
	}
	if item.Type == "document" || item.Type == "documents" {
		reference = "document:" + item.ProjectID + "/" + item.SourceID
		authority = "documents"
	}
	if item.Title == "" {
		item.Title = item.SourceID
	}
	if item.Kind == "" {
		item.Kind = item.Type
	}
	ref := assignmentManifestRef{Reference: reference, Title: boundedManifestText(item.Title, 512), Summary: boundedManifestText(item.Summary, 2048), Kind: item.Kind, Authority: authority, AuthorityState: "suggested", Tags: trimStrings(item.Tags), UpdateMarker: item.UpdatedAt, ReadCostBytes: len(item.Summary), SelectionSource: "librarian", ReadPolicy: "on_demand", ReadWhen: boundedManifestText(item.WhyRelevant, 1024), Group: "nearby-maps"}
	if item.Revision > 0 {
		revision := item.Revision
		ref.Revision = &revision
	}
	return assignmentManifestCandidate{ref: ref}, true
}

func assignmentRelevant(ref assignmentManifestRef, assignment, background string) bool {
	if ref.ReadPolicy == "must_read" {
		return true
	}
	needles := assignmentTerms(assignment + " " + background)
	if len(needles) == 0 {
		return false
	}
	values := append([]string{ref.Reference, ref.Title, ref.Summary, ref.ReadWhen}, ref.Tags...)
	haystack := strings.ToLower(strings.Join(values, " "))
	for _, token := range needles {
		if strings.Contains(haystack, token) {
			return true
		}
	}
	return false
}

func assignmentTerms(value string) []string {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r < 'a' || r > 'z' })
	stop := map[string]struct{}{"about": {}, "assignment": {}, "context": {}, "implement": {}, "read": {}, "that": {}, "this": {}, "with": {}, "from": {}, "into": {}, "using": {}, "task": {}, "code": {}}
	result := make([]string, 0, len(words))
	for _, word := range words {
		if len(word) >= 4 {
			if _, ignored := stop[word]; !ignored {
				result = append(result, word)
			}
		}
	}
	return sortedUniqueStrings(result)
}

func chooseManifestReferences(candidates []assignmentManifestCandidate, limit int) []assignmentManifestRef {
	unique := deduplicateManifestCandidates(candidates)
	if len(unique) > limit {
		unique = unique[:limit]
	}
	result := make([]assignmentManifestRef, 0, len(unique))
	for _, candidate := range unique {
		ref := candidate.ref
		if ref.Group == "" {
			ref.Group = manifestGroup(ref)
		}
		result = append(result, ref)
	}
	return result
}

func deduplicateManifestCandidates(candidates []assignmentManifestCandidate) []assignmentManifestCandidate {
	byReference := make(map[string]assignmentManifestCandidate, len(candidates))
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate.ref.Reference)
		if key == "" {
			continue
		}
		existing, found := byReference[key]
		if !found || manifestCandidateLess(candidate, existing) {
			byReference[key] = candidate
		}
	}
	result := make([]assignmentManifestCandidate, 0, len(byReference))
	for _, candidate := range byReference {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return manifestCandidateLess(result[i], result[j]) })
	return result
}

func manifestCandidateLess(left, right assignmentManifestCandidate) bool {
	leftGroup, rightGroup := manifestGroup(left.ref), manifestGroup(right.ref)
	if manifestGroupRank(leftGroup) != manifestGroupRank(rightGroup) {
		return manifestGroupRank(leftGroup) < manifestGroupRank(rightGroup)
	}
	if manifestSourceRank(left.ref.SelectionSource) != manifestSourceRank(right.ref.SelectionSource) {
		return manifestSourceRank(left.ref.SelectionSource) < manifestSourceRank(right.ref.SelectionSource)
	}
	if left.specificity != right.specificity {
		return left.specificity > right.specificity
	}
	if left.priority != right.priority {
		return left.priority > right.priority
	}
	if left.sort != right.sort {
		return left.sort < right.sort
	}
	if left.ref.Reference != right.ref.Reference {
		return left.ref.Reference < right.ref.Reference
	}
	return manifestCandidateTieKey(left.ref) < manifestCandidateTieKey(right.ref)
}

func manifestCandidateTieKey(ref assignmentManifestRef) string {
	return strings.Join([]string{ref.SelectionSource, ref.Authority, ref.AuthorityState, ref.Title, ref.Summary, ref.ReadWhen, ref.ReadPolicy}, "\x00")
}

func boundedManifestText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	cut := maximum
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut]
}

func manifestGroup(ref assignmentManifestRef) string {
	if ref.Group != "" {
		return ref.Group
	}
	if ref.ReadPolicy == "must_read" {
		return "must-read"
	}
	if ref.SelectionSource == "explicit" || ref.SelectionSource == "inherited_manifest" {
		return "task-local"
	}
	return "likely-useful"
}

func manifestGroupRank(group string) int {
	switch group {
	case "must-read":
		return 0
	case "task-local":
		return 1
	case "likely-useful":
		return 2
	case "nearby-maps":
		return 3
	default:
		return 4
	}
}

func manifestScopeSpecificity(scope string) int {
	switch scope {
	case "task":
		return 5
	case "agent_profile":
		return 4
	case "capability":
		return 3
	case "project":
		return 2
	case "global":
		return 1
	default:
		return 0
	}
}

func manifestSourceRank(source string) int {
	switch source {
	case "explicit":
		return 0
	case "inherited_manifest":
		return 1
	case "knowledge_binding", "legacy_document_guidance":
		return 2
	case "librarian":
		return 3
	default:
		return 4
	}
}

func renderAssignmentManifestMarkdown(response assignmentManifestResponse) string {
	var builder strings.Builder
	builder.WriteString("## Assignment Context\n\n")
	builder.WriteString("### Instructions\n\n")
	builder.WriteString(response.Assignment)
	builder.WriteString("\n")
	if response.Background != "" {
		builder.WriteString("\n### Optional background\n\n")
		builder.WriteString(response.Background)
		builder.WriteString("\n")
	}
	for _, group := range []string{"must-read", "task-local", "likely-useful", "nearby-maps"} {
		entries := make([]assignmentManifestRef, 0)
		for _, ref := range response.References {
			if ref.Group == group {
				entries = append(entries, ref)
			}
		}
		if len(entries) == 0 {
			continue
		}
		builder.WriteString("\n### ")
		builder.WriteString(strings.ReplaceAll(group, "-", " "))
		builder.WriteString("\n\n")
		for _, ref := range entries {
			builder.WriteString("- `")
			builder.WriteString(ref.Reference)
			builder.WriteString("` — ")
			builder.WriteString(ref.Title)
			if ref.ReadWhen != "" {
				builder.WriteString(" (read when: ")
				builder.WriteString(ref.ReadWhen)
				builder.WriteString(")")
			}
			builder.WriteString("\n")
		}
	}
	return builder.String()
}
