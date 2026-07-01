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
