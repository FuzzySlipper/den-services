package guidance

import (
	"time"
)

type AddEntryRequest struct {
	DocumentProjectID string   `json:"document_project_id,omitempty"`
	DocumentSlug      string   `json:"document_slug"`
	Importance        string   `json:"importance,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	SortOrder         int      `json:"sort_order,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

type EntryResponse struct {
	ID                int64     `json:"id"`
	ProjectID         string    `json:"project_id"`
	DocumentProjectID string    `json:"document_project_id"`
	DocumentSlug      string    `json:"document_slug"`
	Importance        string    `json:"importance"`
	Audience          []string  `json:"audience,omitempty"`
	SortOrder         int       `json:"sort_order"`
	Notes             string    `json:"notes,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type EntryListResponse struct {
	Entries []EntryResponse `json:"entries"`
	Count   int             `json:"count"`
}

type GuidancePacketResponse struct {
	ProjectID       string                   `json:"project_id"`
	ResolvedAt      time.Time                `json:"resolved_at"`
	Sources         []GuidanceSourceResponse `json:"sources"`
	SkippedSources  []SkippedSourceResponse  `json:"skipped_sources"`
	ContentMarkdown string                   `json:"content_markdown,omitempty"`
	ContentSHA256   string                   `json:"content_sha256"`
	ContentBytes    int                      `json:"content_bytes"`
	Truncated       bool                     `json:"truncated"`
	Incomplete      bool                     `json:"incomplete"`
}

type GuidanceSourceResponse struct {
	EntryID           int64     `json:"entry_id"`
	SourceScope       string    `json:"source_scope"`
	DocumentProjectID string    `json:"document_project_id"`
	DocumentSlug      string    `json:"document_slug"`
	DocumentTitle     string    `json:"document_title"`
	DocumentType      string    `json:"document_type"`
	DocumentUpdatedAt time.Time `json:"document_updated_at"`
	Visibility        string    `json:"visibility"`
	Tags              []string  `json:"tags,omitempty"`
	Importance        string    `json:"importance"`
	Audience          []string  `json:"audience,omitempty"`
	SortOrder         int       `json:"sort_order"`
	Notes             string    `json:"notes,omitempty"`
	ContentBytes      int       `json:"content_bytes,omitempty"`
}

type SkippedSourceResponse struct {
	EntryID           int64  `json:"entry_id"`
	SourceScope       string `json:"source_scope"`
	DocumentProjectID string `json:"document_project_id"`
	DocumentSlug      string `json:"document_slug"`
	Importance        string `json:"importance"`
	Reason            string `json:"reason"`
	Required          bool   `json:"required"`
}

type DocumentReferencesResponse struct {
	References   []DocumentReferenceResponse `json:"references"`
	ReferencedBy []DocumentReferenceResponse `json:"referenced_by"`
	Count        int                         `json:"count"`
}

type DocumentReferenceResponse struct {
	RefKind        string    `json:"ref_kind"`
	Description    string    `json:"description"`
	ScopeProjectID string    `json:"scope_project_id"`
	EntryID        int64     `json:"entry_id"`
	Importance     string    `json:"importance"`
	Audience       []string  `json:"audience,omitempty"`
	SortOrder      int       `json:"sort_order"`
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DeleteResponse struct {
	Deleted bool   `json:"deleted"`
	Message string `json:"message"`
}

type CreateKnowledgeBindingRequest struct {
	TargetKind    string   `json:"target_kind,omitempty"`
	TargetRef     string   `json:"target_ref"`
	ScopeKind     string   `json:"scope_kind"`
	ScopeRef      string   `json:"scope_ref"`
	Audience      []string `json:"audience,omitempty"`
	Priority      int      `json:"priority,omitempty"`
	SortOrder     int      `json:"sort_order,omitempty"`
	ReadPolicy    string   `json:"read_policy,omitempty"`
	ReadWhen      string   `json:"read_when,omitempty"`
	ShadowsGlobal bool     `json:"shadows_global,omitempty"`
}

type UpdateKnowledgeBindingRequest struct {
	ScopeKind     string   `json:"scope_kind"`
	ScopeRef      string   `json:"scope_ref"`
	Audience      []string `json:"audience,omitempty"`
	Priority      int      `json:"priority,omitempty"`
	SortOrder     int      `json:"sort_order,omitempty"`
	ReadPolicy    string   `json:"read_policy,omitempty"`
	ReadWhen      string   `json:"read_when,omitempty"`
	ShadowsGlobal bool     `json:"shadows_global,omitempty"`
}

type BindingResolveRequest struct {
	Scopes       []BindingScope `json:"scopes,omitempty"`
	Audience     []string       `json:"audience,omitempty"`
	InlineBudget int            `json:"inline_budget,omitempty"`
}

type ContextResolveRequest struct {
	ProjectID string `json:"project_id"`
	BindingResolveRequest
}

type KnowledgeMetadataResponse struct {
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary,omitempty"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	CurationState   string     `json:"curation_state"`
	ReplacementSlug string     `json:"replacement_slug,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastReviewedAt  *time.Time `json:"last_reviewed_at,omitempty"`
	Revision        int        `json:"revision,omitempty"`
	RevisionKnown   bool       `json:"revision_known"`
}

type KnowledgeBindingResponse struct {
	ID            int64     `json:"id"`
	TargetKind    string    `json:"target_kind"`
	TargetRef     string    `json:"target_ref"`
	ScopeKind     string    `json:"scope_kind"`
	ScopeRef      string    `json:"scope_ref"`
	Audience      []string  `json:"audience,omitempty"`
	Priority      int       `json:"priority"`
	SortOrder     int       `json:"sort_order"`
	ReadPolicy    string    `json:"read_policy"`
	ReadWhen      string    `json:"read_when,omitempty"`
	ShadowsGlobal bool      `json:"shadows_global"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type KnowledgeBindingListResponse struct {
	Bindings   []KnowledgeBindingResponse `json:"bindings"`
	Count      int                        `json:"count"`
	NextOffset *int                       `json:"next_offset,omitempty"`
}

type ResolvedKnowledgeBindingResponse struct {
	KnowledgeBindingResponse
	SelectionSource string                     `json:"selection_source"`
	Target          *KnowledgeMetadataResponse `json:"target,omitempty"`
	TargetState     string                     `json:"target_state"`
	InlineAdmitted  bool                       `json:"inline_admitted"`
}

type BindingResolutionResponse struct {
	ResolvedAt       time.Time                          `json:"resolved_at"`
	Selections       []ResolvedKnowledgeBindingResponse `json:"selections"`
	ExcludedBindings []ResolvedKnowledgeBindingResponse `json:"excluded_bindings"`
	InlineBudget     int                                `json:"inline_budget"`
	InlineUsed       int                                `json:"inline_used"`
	Truncated        bool                               `json:"truncated"`
}

func toEntryResponse(entry Entry) EntryResponse {
	return EntryResponse{
		ID:                entry.ID,
		ProjectID:         entry.ProjectID,
		DocumentProjectID: entry.DocumentProjectID,
		DocumentSlug:      entry.DocumentSlug,
		Importance:        entry.Importance,
		Audience:          append([]string(nil), entry.Audience...),
		SortOrder:         entry.SortOrder,
		Notes:             entry.Notes,
		CreatedAt:         entry.CreatedAt,
		UpdatedAt:         entry.UpdatedAt,
	}
}

func toEntryResponses(entries []Entry) []EntryResponse {
	responses := make([]EntryResponse, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, toEntryResponse(entry))
	}
	return responses
}

func toPacketResponse(packet GuidancePacket) GuidancePacketResponse {
	sources := make([]GuidanceSourceResponse, 0, len(packet.Sources))
	for _, source := range packet.Sources {
		sources = append(sources, GuidanceSourceResponse(source))
	}
	skipped := make([]SkippedSourceResponse, 0, len(packet.SkippedSources))
	for _, source := range packet.SkippedSources {
		skipped = append(skipped, SkippedSourceResponse(source))
	}
	return GuidancePacketResponse{
		ProjectID:       packet.ProjectID,
		ResolvedAt:      packet.ResolvedAt,
		Sources:         sources,
		SkippedSources:  skipped,
		ContentMarkdown: packet.ContentMarkdown,
		ContentSHA256:   packet.ContentSHA256,
		ContentBytes:    packet.ContentBytes,
		Truncated:       packet.Truncated,
		Incomplete:      packet.Incomplete,
	}
}

func toDocumentReferenceResponses(refs []DocumentReference) []DocumentReferenceResponse {
	responses := make([]DocumentReferenceResponse, 0, len(refs))
	for _, ref := range refs {
		responses = append(responses, DocumentReferenceResponse(ref))
	}
	return responses
}

func toKnowledgeBindingResponse(binding KnowledgeBinding) KnowledgeBindingResponse {
	return KnowledgeBindingResponse{ID: binding.ID, TargetKind: binding.TargetKind, TargetRef: binding.TargetRef, ScopeKind: binding.ScopeKind, ScopeRef: binding.ScopeRef, Audience: append([]string(nil), binding.Audience...), Priority: binding.Priority, SortOrder: binding.SortOrder, ReadPolicy: binding.ReadPolicy, ReadWhen: binding.ReadWhen, ShadowsGlobal: binding.ShadowsGlobal, CreatedAt: binding.CreatedAt, UpdatedAt: binding.UpdatedAt}
}

func toKnowledgeBindingResponses(bindings []KnowledgeBinding) []KnowledgeBindingResponse {
	responses := make([]KnowledgeBindingResponse, 0, len(bindings))
	for _, binding := range bindings {
		responses = append(responses, toKnowledgeBindingResponse(binding))
	}
	return responses
}

func toBindingResolutionResponse(resolution BindingResolution) BindingResolutionResponse {
	response := BindingResolutionResponse{ResolvedAt: resolution.ResolvedAt, InlineBudget: resolution.InlineBudget, InlineUsed: resolution.InlineUsed, Truncated: resolution.Truncated, Selections: make([]ResolvedKnowledgeBindingResponse, 0, len(resolution.Selections)), ExcludedBindings: make([]ResolvedKnowledgeBindingResponse, 0, len(resolution.ExcludedBindings))}
	for _, item := range resolution.Selections {
		response.Selections = append(response.Selections, toResolvedKnowledgeBindingResponse(item))
	}
	for _, item := range resolution.ExcludedBindings {
		response.ExcludedBindings = append(response.ExcludedBindings, toResolvedKnowledgeBindingResponse(item))
	}
	return response
}

func toResolvedKnowledgeBindingResponse(item ResolvedKnowledgeBinding) ResolvedKnowledgeBindingResponse {
	response := ResolvedKnowledgeBindingResponse{KnowledgeBindingResponse: toKnowledgeBindingResponse(item.Binding), SelectionSource: item.SelectionSource, TargetState: item.TargetState, InlineAdmitted: item.InlineAdmitted}
	if item.Target != nil {
		response.Target = &KnowledgeMetadataResponse{Slug: item.Target.Slug, Title: item.Target.Title, Summary: item.Target.Summary, Kind: item.Target.Kind, Status: item.Target.Status, CurationState: item.Target.CurationState, ReplacementSlug: item.Target.ReplacementSlug, UpdatedAt: item.Target.UpdatedAt, LastReviewedAt: item.Target.LastReviewedAt, Revision: item.Target.Revision, RevisionKnown: item.Target.RevisionKnown}
	}
	return response
}
