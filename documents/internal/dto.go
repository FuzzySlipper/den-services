package documents

import (
	"encoding/json"
	"time"
)

type StoreDocumentRequest struct {
	Slug    string   `json:"slug"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	DocType string   `json:"doc_type,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Summary string   `json:"summary,omitempty"`
}

type UpdateVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

type CommentOnDocumentRequest struct {
	AuthorIdentity  string          `json:"author_identity"`
	BodyMarkdown    string          `json:"body_markdown"`
	ParentCommentID *int64          `json:"parent_comment_id,omitempty"`
	CommentKind     string          `json:"comment_kind,omitempty"`
	Anchor          string          `json:"anchor,omitempty"`
	Mentions        json.RawMessage `json:"mentions,omitempty"`
	SourceRefs      json.RawMessage `json:"source_refs,omitempty"`
}

type CreateThreadRequest struct {
	TargetType      string          `json:"target_type"`
	TargetProjectID string          `json:"target_project_id"`
	TargetSlug      string          `json:"target_slug"`
	ThreadKey       string          `json:"thread_key"`
	Title           string          `json:"title"`
	CreatedBy       string          `json:"created_by"`
	Summary         string          `json:"summary,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type CreateCommentRequest struct {
	AuthorIdentity  string          `json:"author_identity"`
	BodyMarkdown    string          `json:"body_markdown"`
	ParentCommentID *int64          `json:"parent_comment_id,omitempty"`
	CommentKind     string          `json:"comment_kind,omitempty"`
	Mentions        json.RawMessage `json:"mentions,omitempty"`
	SourceRefs      json.RawMessage `json:"source_refs,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type UpdateThreadRequest struct {
	Status            *string         `json:"status,omitempty"`
	Title             *string         `json:"title,omitempty"`
	Summary           *string         `json:"summary,omitempty"`
	ResolutionSummary *string         `json:"resolution_summary,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
}

type DocumentResponse struct {
	ID         int64     `json:"id"`
	ProjectID  string    `json:"project_id"`
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	DocType    string    `json:"doc_type"`
	Visibility string    `json:"visibility"`
	Tags       []string  `json:"tags,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DocumentSummaryResponse struct {
	ID         int64     `json:"id"`
	ProjectID  string    `json:"project_id"`
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	DocType    string    `json:"doc_type"`
	Visibility string    `json:"visibility"`
	Tags       []string  `json:"tags,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DocumentSearchResponse struct {
	ProjectID  string  `json:"project_id"`
	Slug       string  `json:"slug"`
	Title      string  `json:"title"`
	DocType    string  `json:"doc_type"`
	Visibility string  `json:"visibility"`
	Summary    string  `json:"summary,omitempty"`
	Snippet    string  `json:"snippet"`
	Rank       float64 `json:"rank"`
}

type ArchivePreflightResponse struct {
	ProjectID                   string                      `json:"project_id"`
	Slug                        string                      `json:"slug"`
	CanArchive                  bool                        `json:"can_archive"`
	ReferencedBy                []DocumentReferenceResponse `json:"referenced_by"`
	GuidanceReferenceCheckReady bool                        `json:"guidance_reference_check_ready"`
}

type DocumentReferenceResponse struct {
	RefKind        string `json:"ref_kind"`
	Description    string `json:"description"`
	ScopeProjectID string `json:"scope_project_id,omitempty"`
}

type DiscussionThreadResponse struct {
	ID                int64           `json:"id"`
	TargetType        string          `json:"target_type"`
	TargetProjectID   string          `json:"target_project_id"`
	TargetID          *int64          `json:"target_id,omitempty"`
	TargetSlug        string          `json:"target_slug,omitempty"`
	TargetAnchor      string          `json:"target_anchor,omitempty"`
	ThreadKey         string          `json:"thread_key"`
	Title             string          `json:"title"`
	Status            string          `json:"status"`
	CreatedBy         string          `json:"created_by"`
	Summary           string          `json:"summary,omitempty"`
	ResolutionSummary string          `json:"resolution_summary,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	LastCommentAt     *time.Time      `json:"last_comment_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type DiscussionCommentResponse struct {
	ID              int64           `json:"id"`
	ThreadID        int64           `json:"thread_id"`
	ParentCommentID *int64          `json:"parent_comment_id,omitempty"`
	AuthorIdentity  string          `json:"author_identity"`
	BodyMarkdown    string          `json:"body_markdown"`
	CommentKind     string          `json:"comment_kind"`
	Status          string          `json:"status"`
	Mentions        json.RawMessage `json:"mentions,omitempty"`
	SourceRefs      json.RawMessage `json:"source_refs,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	EditedAt        *time.Time      `json:"edited_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type DiscussionDetailResponse struct {
	DocumentID    int64                       `json:"document_id"`
	ProjectID     string                      `json:"project_id"`
	Slug          string                      `json:"slug"`
	Threads       []DiscussionThreadResponse  `json:"threads"`
	DefaultThread *DiscussionThreadResponse   `json:"default_thread,omitempty"`
	Comments      []DiscussionCommentResponse `json:"comments"`
}

type ArchivedDocumentsResponse struct {
	Documents []DocumentSummaryResponse `json:"documents,omitempty"`
	Results   []DocumentSearchResponse  `json:"results,omitempty"`
}

type SimpleMessageResponse struct {
	Message string `json:"message"`
}

func toDocumentResponse(doc *Document) DocumentResponse {
	return DocumentResponse{
		ID:         doc.ID(),
		ProjectID:  doc.ProjectID(),
		Slug:       doc.Slug(),
		Title:      doc.Title(),
		Content:    doc.Content(),
		DocType:    doc.DocType(),
		Visibility: doc.Visibility(),
		Tags:       doc.Tags(),
		Summary:    doc.Summary(),
		CreatedAt:  doc.CreatedAt(),
		UpdatedAt:  doc.UpdatedAt(),
	}
}

func toSummaryResponse(summary DocumentSummary) DocumentSummaryResponse {
	return DocumentSummaryResponse(summary)
}

func toSummaryResponses(summaries []DocumentSummary) []DocumentSummaryResponse {
	responses := make([]DocumentSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		responses = append(responses, toSummaryResponse(summary))
	}
	return responses
}

func toSearchResponse(result DocumentSearchResult) DocumentSearchResponse {
	return DocumentSearchResponse(result)
}

func toSearchResponses(results []DocumentSearchResult) []DocumentSearchResponse {
	responses := make([]DocumentSearchResponse, 0, len(results))
	for _, result := range results {
		responses = append(responses, toSearchResponse(result))
	}
	return responses
}

func toArchivePreflightResponse(result ArchivePreflightResult) ArchivePreflightResponse {
	refs := make([]DocumentReferenceResponse, 0, len(result.ReferencedBy))
	for _, ref := range result.ReferencedBy {
		refs = append(refs, DocumentReferenceResponse(ref))
	}
	return ArchivePreflightResponse{
		ProjectID:                   result.ProjectID,
		Slug:                        result.Slug,
		CanArchive:                  result.CanArchive,
		ReferencedBy:                refs,
		GuidanceReferenceCheckReady: result.GuidanceReferenceCheckReady,
	}
}

func toThreadResponse(thread DiscussionThread) DiscussionThreadResponse {
	return DiscussionThreadResponse{
		ID:                thread.ID,
		TargetType:        thread.TargetType,
		TargetProjectID:   thread.TargetProjectID,
		TargetID:          thread.TargetID,
		TargetSlug:        thread.TargetSlug,
		TargetAnchor:      thread.TargetAnchor,
		ThreadKey:         thread.ThreadKey,
		Title:             thread.Title,
		Status:            thread.Status,
		CreatedBy:         thread.CreatedBy,
		Summary:           thread.Summary,
		ResolutionSummary: thread.ResolutionSummary,
		Metadata:          cloneBytes(thread.MetadataJSON),
		LastCommentAt:     thread.LastCommentAt,
		CreatedAt:         thread.CreatedAt,
		UpdatedAt:         thread.UpdatedAt,
	}
}

func toThreadResponses(threads []DiscussionThread) []DiscussionThreadResponse {
	responses := make([]DiscussionThreadResponse, 0, len(threads))
	for _, thread := range threads {
		responses = append(responses, toThreadResponse(thread))
	}
	return responses
}

func toCommentResponse(comment *DiscussionComment) DiscussionCommentResponse {
	return DiscussionCommentResponse{
		ID:              comment.ID,
		ThreadID:        comment.ThreadID,
		ParentCommentID: comment.ParentCommentID,
		AuthorIdentity:  comment.AuthorIdentity,
		BodyMarkdown:    comment.BodyMarkdown,
		CommentKind:     comment.CommentKind,
		Status:          comment.Status,
		Mentions:        cloneBytes(comment.MentionsJSON),
		SourceRefs:      cloneBytes(comment.SourceRefsJSON),
		Metadata:        cloneBytes(comment.MetadataJSON),
		CreatedAt:       comment.CreatedAt,
		EditedAt:        comment.EditedAt,
		UpdatedAt:       comment.UpdatedAt,
	}
}

func toCommentResponses(comments []DiscussionComment) []DiscussionCommentResponse {
	responses := make([]DiscussionCommentResponse, 0, len(comments))
	for i := range comments {
		responses = append(responses, toCommentResponse(&comments[i]))
	}
	return responses
}

func toDiscussionDetailResponse(detail DiscussionDetail) DiscussionDetailResponse {
	var defaultThread *DiscussionThreadResponse
	if detail.DefaultThread != nil {
		converted := toThreadResponse(*detail.DefaultThread)
		defaultThread = &converted
	}
	return DiscussionDetailResponse{
		DocumentID:    detail.DocumentID,
		ProjectID:     detail.ProjectID,
		Slug:          detail.Slug,
		Threads:       toThreadResponses(detail.Threads),
		DefaultThread: defaultThread,
		Comments:      toCommentResponses(detail.Comments),
	}
}
