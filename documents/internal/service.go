package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type GuidanceReferenceReader interface {
	DocumentReferences(ctx context.Context, projectID string, slug string) ([]DocumentReference, bool, error)
}

type DocumentStore interface {
	Ping(ctx context.Context) error
	UpsertDocument(ctx context.Context, document *Document) (*Document, error)
	GetDocument(ctx context.Context, projectID string, slug string) (*Document, error)
	ListDocuments(ctx context.Context, query ListDocumentsQuery) ([]DocumentSummary, error)
	SearchDocuments(ctx context.Context, query string, projectID string, visibility string) ([]DocumentSearchResult, error)
	DeleteDocument(ctx context.Context, projectID string, slug string) (bool, error)
	UpdateVisibility(ctx context.Context, projectID string, slug string, visibility string, updatedAt time.Time) (*Document, error)
	GetOrCreateThread(ctx context.Context, projectID string, slug string, threadKey string, title string, createdBy string, targetAnchor string, now time.Time) (*DiscussionThread, error)
	CreateThread(ctx context.Context, thread DiscussionThread) (*DiscussionThread, error)
	GetThread(ctx context.Context, id int64) (*DiscussionThread, error)
	ListThreads(ctx context.Context, query ListThreadsQuery) ([]DiscussionThread, error)
	UpdateThread(ctx context.Context, id int64, patch ThreadPatch, updatedAt time.Time) (*DiscussionThread, error)
	AddComment(ctx context.Context, comment DiscussionComment, now time.Time) (*DiscussionComment, error)
	GetComment(ctx context.Context, id int64) (*DiscussionComment, error)
	ListComments(ctx context.Context, threadID int64) ([]DiscussionComment, error)
}

type ThreadPatch struct {
	Status            *string
	Title             *string
	Summary           *string
	ResolutionSummary *string
	MetadataJSON      []byte
	HasMetadata       bool
}

type Service struct {
	store    DocumentStore
	projects ProjectValidator
	guidance GuidanceReferenceReader
	clock    func() time.Time
}

func NewService(store DocumentStore, projects ProjectValidator, guidance GuidanceReferenceReader, clock func() time.Time) *Service {
	return &Service{store: store, projects: projects, guidance: guidance, clock: clock}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) EnsureDocumentDiscussion(ctx context.Context, projectID string, slug string) (DiscussionDetail, error) {
	return s.GetDocumentDiscussion(ctx, projectID, slug, true, false, "")
}

func (s *Service) StoreDocument(ctx context.Context, projectID string, req StoreDocumentRequest) (*Document, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	doc, err := NewDocument(NewDocumentParams{
		ProjectID: projectID,
		Slug:      req.Slug,
		Title:     req.Title,
		Content:   req.Content,
		DocType:   req.DocType,
		Tags:      req.Tags,
		Summary:   req.Summary,
		CreatedAt: s.clock().UTC(),
		UpdatedAt: s.clock().UTC(),
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.UpsertDocument(ctx, doc)
}

func (s *Service) GetDocument(ctx context.Context, projectID string, slug string) (*Document, error) {
	projectID, slug, err := validateProjectSlug(projectID, slug)
	if err != nil {
		return nil, err
	}
	return s.store.GetDocument(ctx, projectID, slug)
}

func (s *Service) ListDocuments(ctx context.Context, query ListDocumentsQuery) ([]DocumentSummary, error) {
	if query.DocType != "" && !validDocType(query.DocType) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidDocType, query.DocType))
	}
	if query.HasVisibility && !validVisibility(query.Visibility) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidVisibility, query.Visibility))
	}
	query.Tags = normalizeTags(query.Tags)
	return s.store.ListDocuments(ctx, query)
}

func (s *Service) SearchDocuments(ctx context.Context, query string, projectID string) ([]DocumentSearchResult, error) {
	return s.search(ctx, query, projectID, VisibilityNormal)
}

func (s *Service) QueryArchivedDocuments(ctx context.Context, query string, projectID string, docType string, tags []string) ([]DocumentSummary, []DocumentSearchResult, error) {
	if strings.TrimSpace(query) != "" {
		results, err := s.search(ctx, query, projectID, VisibilityArchived)
		return nil, results, err
	}
	docs, err := s.ListDocuments(ctx, ListDocumentsQuery{
		ProjectID:     strings.TrimSpace(projectID),
		DocType:       strings.TrimSpace(docType),
		Tags:          tags,
		Visibility:    VisibilityArchived,
		HasVisibility: true,
	})
	return docs, nil, err
}

func (s *Service) DeleteDocument(ctx context.Context, projectID string, slug string) (bool, error) {
	projectID, slug, err := validateProjectSlug(projectID, slug)
	if err != nil {
		return false, err
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return false, err
	}
	return s.store.DeleteDocument(ctx, projectID, slug)
}

func (s *Service) UpdateVisibility(ctx context.Context, projectID string, slug string, visibility string) (*Document, error) {
	projectID, slug, err := validateProjectSlug(projectID, slug)
	if err != nil {
		return nil, err
	}
	visibility = strings.TrimSpace(visibility)
	if !validVisibility(visibility) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidVisibility, visibility))
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	return s.store.UpdateVisibility(ctx, projectID, slug, visibility, s.clock().UTC())
}

func (s *Service) ArchivePreflight(ctx context.Context, projectID string, slug string) (ArchivePreflightResult, error) {
	projectID, slug, err := validateProjectSlug(projectID, slug)
	if err != nil {
		return ArchivePreflightResult{}, err
	}
	if _, err := s.store.GetDocument(ctx, projectID, slug); err != nil {
		return ArchivePreflightResult{ProjectID: projectID, Slug: slug, CanArchive: false}, err
	}
	refs, ready, err := s.guidance.DocumentReferences(ctx, projectID, slug)
	if err != nil {
		return ArchivePreflightResult{}, err
	}
	return ArchivePreflightResult{
		ProjectID:                   projectID,
		Slug:                        slug,
		CanArchive:                  len(refs) == 0 && ready,
		ReferencedBy:                refs,
		GuidanceReferenceCheckReady: ready,
	}, nil
}

func (s *Service) GetDocumentDiscussion(ctx context.Context, projectID string, slug string, createIfMissing bool, includeResolved bool, anchor string) (DiscussionDetail, error) {
	doc, err := s.GetDocument(ctx, projectID, slug)
	if err != nil {
		return DiscussionDetail{}, err
	}
	threadKey := threadKeyForAnchor(anchor)
	var selected *DiscussionThread
	if createIfMissing {
		if err := s.projects.AssertWritable(ctx, doc.ProjectID()); err != nil {
			return DiscussionDetail{}, err
		}
		selected, err = s.store.GetOrCreateThread(ctx, doc.ProjectID(), doc.Slug(), threadKey, threadTitle(doc.Slug(), threadKey), "mcp-agent", strings.TrimSpace(anchor), s.clock().UTC())
		if err != nil {
			return DiscussionDetail{}, err
		}
	} else {
		threads, err := s.store.ListThreads(ctx, ListThreadsQuery{TargetType: TargetTypeDocument, TargetProjectID: doc.ProjectID(), TargetSlug: doc.Slug()})
		if err != nil {
			return DiscussionDetail{}, err
		}
		for i := range threads {
			if threads[i].ThreadKey == threadKey {
				selected = &threads[i]
				break
			}
		}
	}
	if selected == nil {
		return DiscussionDetail{DocumentID: doc.ID(), ProjectID: doc.ProjectID(), Slug: doc.Slug()}, nil
	}
	threads, err := s.store.ListThreads(ctx, ListThreadsQuery{TargetType: TargetTypeDocument, TargetProjectID: doc.ProjectID(), TargetSlug: doc.Slug()})
	if err != nil {
		return DiscussionDetail{}, err
	}
	filtered := make([]DiscussionThread, 0, len(threads))
	for _, thread := range threads {
		if anchor != "" && thread.ThreadKey != threadKey {
			continue
		}
		if !includeResolved && thread.Status != ThreadStatusOpen {
			continue
		}
		filtered = append(filtered, thread)
	}
	comments, err := s.store.ListComments(ctx, selected.ID)
	if err != nil {
		return DiscussionDetail{}, err
	}
	return DiscussionDetail{DocumentID: doc.ID(), ProjectID: doc.ProjectID(), Slug: doc.Slug(), Threads: filtered, DefaultThread: selected, Comments: comments}, nil
}

func (s *Service) CommentOnDocument(ctx context.Context, projectID string, slug string, req CommentOnDocumentRequest) (*DiscussionComment, *DiscussionThread, error) {
	doc, err := s.GetDocument(ctx, projectID, slug)
	if err != nil {
		return nil, nil, err
	}
	if err := s.projects.AssertWritable(ctx, doc.ProjectID()); err != nil {
		return nil, nil, err
	}
	threadKey := threadKeyForAnchor(req.Anchor)
	thread, err := s.store.GetOrCreateThread(ctx, doc.ProjectID(), doc.Slug(), threadKey, threadTitle(doc.Slug(), threadKey), req.AuthorIdentity, strings.TrimSpace(req.Anchor), s.clock().UTC())
	if err != nil {
		return nil, nil, err
	}
	comment, err := s.createComment(ctx, thread.ID, req.ParentCommentID, req.AuthorIdentity, req.BodyMarkdown, req.CommentKind, req.Mentions, req.SourceRefs, nil)
	return comment, thread, err
}

func (s *Service) CreateDiscussionComment(ctx context.Context, threadID int64, req CreateCommentRequest) (*DiscussionComment, error) {
	return s.createComment(ctx, threadID, req.ParentCommentID, req.AuthorIdentity, req.BodyMarkdown, req.CommentKind, req.Mentions, req.SourceRefs, req.Metadata)
}

func (s *Service) CreateDiscussionThread(ctx context.Context, req CreateThreadRequest) (*DiscussionThread, error) {
	targetType := strings.TrimSpace(req.TargetType)
	if targetType == "" {
		targetType = TargetTypeDocument
	}
	if targetType != TargetTypeDocument {
		return nil, validationFailed(ErrInvalidTargetType)
	}
	projectID, slug, err := validateProjectSlug(req.TargetProjectID, req.TargetSlug)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetDocument(ctx, projectID, slug); err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	threadKey := strings.TrimSpace(req.ThreadKey)
	if threadKey == "" {
		threadKey = DefaultThreadKey
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, validationFailed(ErrMissingTitle)
	}
	createdBy := strings.TrimSpace(req.CreatedBy)
	if createdBy == "" {
		return nil, validationFailed(ErrMissingAuthor)
	}
	metadata, err := encodeOptionalJSON(req.Metadata)
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreateThread(ctx, DiscussionThread{
		TargetType:      TargetTypeDocument,
		TargetProjectID: projectID,
		TargetSlug:      slug,
		ThreadKey:       threadKey,
		Title:           title,
		Status:          ThreadStatusOpen,
		CreatedBy:       createdBy,
		Summary:         strings.TrimSpace(req.Summary),
		MetadataJSON:    metadata,
		CreatedAt:       s.clock().UTC(),
		UpdatedAt:       s.clock().UTC(),
	})
}

func (s *Service) ListDiscussionThreads(ctx context.Context, query ListThreadsQuery) ([]DiscussionThread, error) {
	query.TargetType = strings.TrimSpace(query.TargetType)
	if query.TargetType == "" {
		query.TargetType = TargetTypeDocument
	}
	if query.TargetType != TargetTypeDocument {
		return nil, validationFailed(ErrInvalidTargetType)
	}
	if strings.TrimSpace(query.TargetProjectID) == "" || strings.TrimSpace(query.TargetSlug) == "" {
		return nil, validationFailed(fmt.Errorf("%w and %w are required", ErrMissingProjectID, ErrMissingSlug))
	}
	if query.Status != "" && !validThreadStatus(query.Status) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidThreadStatus, query.Status))
	}
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 50
	}
	return s.store.ListThreads(ctx, query)
}

func (s *Service) GetDiscussionThread(ctx context.Context, id int64, includeComments bool) (*DiscussionThread, []DiscussionComment, error) {
	thread, err := s.store.GetThread(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !includeComments {
		return thread, nil, nil
	}
	comments, err := s.store.ListComments(ctx, id)
	return thread, comments, err
}

func (s *Service) UpdateDiscussionThread(ctx context.Context, id int64, req UpdateThreadRequest) (*DiscussionThread, error) {
	patch := ThreadPatch{}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validThreadStatus(status) {
			return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidThreadStatus, status))
		}
		patch.Status = &status
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, validationFailed(ErrMissingTitle)
		}
		patch.Title = &title
	}
	patch.Summary = trimOptional(req.Summary)
	patch.ResolutionSummary = trimOptional(req.ResolutionSummary)
	if req.Metadata != nil {
		data, err := json.Marshal(req.Metadata)
		if err != nil {
			return nil, validationFailed(ErrInvalidJSON)
		}
		patch.MetadataJSON = data
		patch.HasMetadata = true
	}
	return s.store.UpdateThread(ctx, id, patch, s.clock().UTC())
}

func (s *Service) search(ctx context.Context, query string, projectID string, visibility string) ([]DocumentSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, validationFailed(ErrSearchQueryEmpty)
	}
	return s.store.SearchDocuments(ctx, query, strings.TrimSpace(projectID), visibility)
}

func (s *Service) createComment(ctx context.Context, threadID int64, parentID *int64, author string, body string, kind string, mentions any, sourceRefs any, metadata any) (*DiscussionComment, error) {
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, thread.TargetProjectID); err != nil {
		return nil, err
	}
	author = strings.TrimSpace(author)
	if author == "" {
		return nil, validationFailed(ErrMissingAuthor)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, validationFailed(ErrMissingBody)
	}
	if kind == "" {
		kind = CommentKindComment
	}
	if !validCommentKind(kind) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidCommentKind, kind))
	}
	if parentID != nil {
		parent, err := s.store.GetComment(ctx, *parentID)
		if err != nil {
			return nil, err
		}
		if parent.ThreadID != threadID {
			return nil, conflict(ErrParentThreadMismatch, "parent_thread_mismatch")
		}
	}
	mentionsJSON, err := encodeOptionalJSON(mentions)
	if err != nil {
		return nil, validationFailed(err)
	}
	sourceRefsJSON, err := encodeOptionalJSON(sourceRefs)
	if err != nil {
		return nil, validationFailed(err)
	}
	metadataJSON, err := encodeOptionalJSON(metadata)
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.AddComment(ctx, DiscussionComment{
		ThreadID:        threadID,
		ParentCommentID: parentID,
		AuthorIdentity:  author,
		BodyMarkdown:    body,
		CommentKind:     kind,
		Status:          CommentStatusActive,
		MentionsJSON:    mentionsJSON,
		SourceRefsJSON:  sourceRefsJSON,
		MetadataJSON:    metadataJSON,
	}, s.clock().UTC())
}

func validateProjectSlug(projectID string, slug string) (string, string, error) {
	projectID = strings.TrimSpace(projectID)
	slug = strings.TrimSpace(slug)
	if projectID == "" {
		return "", "", validationFailed(ErrMissingProjectID)
	}
	if slug == "" {
		return "", "", validationFailed(ErrMissingSlug)
	}
	return projectID, slug, nil
}

func threadTitle(slug string, threadKey string) string {
	if threadKey == DefaultThreadKey {
		return "Discussion for " + slug
	}
	return "Discussion " + threadKey + " for " + slug
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func encodeOptionalJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		if len(raw) == 0 || string(raw) == "null" {
			return nil, nil
		}
		if !json.Valid(raw) {
			return nil, ErrInvalidJSON
		}
		return cloneBytes(raw), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidJSON
	}
	if string(data) == "null" {
		return nil, nil
	}
	return data, nil
}
