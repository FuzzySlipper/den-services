package documents

import (
	"context"
	"strings"
	"sync"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	nextDoc  int64
	nextThrd int64
	nextComm int64
	docs     []*Document
	threads  []DiscussionThread
	comments []DiscussionComment
}

func newMemoryStore() *memoryStore {
	return &memoryStore{nextDoc: 1, nextThrd: 1, nextComm: 1}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) UpsertDocument(_ context.Context, document *Document) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.docs {
		if existing.ProjectID() == document.ProjectID() && existing.Slug() == document.Slug() {
			updated, err := NewDocument(NewDocumentParams{
				ID:         existing.ID(),
				ProjectID:  document.ProjectID(),
				Slug:       document.Slug(),
				Title:      document.Title(),
				Content:    document.Content(),
				DocType:    document.DocType(),
				Visibility: existing.Visibility(),
				Tags:       document.Tags(),
				Summary:    document.Summary(),
				CreatedAt:  existing.CreatedAt(),
				UpdatedAt:  document.UpdatedAt(),
			})
			if err != nil {
				return nil, err
			}
			s.docs[i] = updated
			return updated, nil
		}
	}
	created, err := NewDocument(NewDocumentParams{
		ID:         s.nextDoc,
		ProjectID:  document.ProjectID(),
		Slug:       document.Slug(),
		Title:      document.Title(),
		Content:    document.Content(),
		DocType:    document.DocType(),
		Visibility: document.Visibility(),
		Tags:       document.Tags(),
		Summary:    document.Summary(),
		CreatedAt:  document.CreatedAt(),
		UpdatedAt:  document.UpdatedAt(),
	})
	if err != nil {
		return nil, err
	}
	s.nextDoc++
	s.docs = append(s.docs, created)
	return created, nil
}

func (s *memoryStore) GetDocument(_ context.Context, projectID string, slug string) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, doc := range s.docs {
		if doc.ProjectID() == projectID && doc.Slug() == slug {
			return doc, nil
		}
	}
	return nil, documentNotFound(projectID, slug)
}

func (s *memoryStore) ListDocuments(_ context.Context, query ListDocumentsQuery) ([]DocumentSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var docs []DocumentSummary
	for _, doc := range s.docs {
		if query.ProjectID != "" && doc.ProjectID() != query.ProjectID {
			continue
		}
		if query.DocType != "" && doc.DocType() != query.DocType {
			continue
		}
		if query.HasVisibility {
			if doc.Visibility() != query.Visibility {
				continue
			}
		} else if doc.Visibility() != VisibilityNormal {
			continue
		}
		if !hasAllTags(doc.Tags(), query.Tags) {
			continue
		}
		docs = append(docs, DocumentSummary{
			ID:         doc.ID(),
			ProjectID:  doc.ProjectID(),
			Slug:       doc.Slug(),
			Title:      doc.Title(),
			DocType:    doc.DocType(),
			Visibility: doc.Visibility(),
			Tags:       doc.Tags(),
			Summary:    doc.Summary(),
			UpdatedAt:  doc.UpdatedAt(),
		})
	}
	return docs, nil
}

func (s *memoryStore) SearchDocuments(_ context.Context, query string, projectID string, visibility string) ([]DocumentSearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []DocumentSearchResult
	query = strings.ToLower(query)
	for _, doc := range s.docs {
		if projectID != "" && doc.ProjectID() != projectID {
			continue
		}
		if doc.Visibility() != visibility {
			continue
		}
		haystack := strings.ToLower(doc.Title() + " " + doc.Summary() + " " + doc.Content() + " " + strings.Join(doc.Tags(), " "))
		if !strings.Contains(haystack, query) {
			continue
		}
		results = append(results, DocumentSearchResult{
			ProjectID:  doc.ProjectID(),
			Slug:       doc.Slug(),
			Title:      doc.Title(),
			DocType:    doc.DocType(),
			Visibility: doc.Visibility(),
			Summary:    doc.Summary(),
			Snippet:    doc.Content(),
			Rank:       1,
		})
	}
	return results, nil
}

func (s *memoryStore) DeleteDocument(_ context.Context, projectID string, slug string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, doc := range s.docs {
		if doc.ProjectID() == projectID && doc.Slug() == slug {
			s.docs = append(s.docs[:i], s.docs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryStore) UpdateVisibility(_ context.Context, projectID string, slug string, visibility string, updatedAt time.Time) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, doc := range s.docs {
		if doc.ProjectID() != projectID || doc.Slug() != slug {
			continue
		}
		updated, err := NewDocument(NewDocumentParams{
			ID:         doc.ID(),
			ProjectID:  doc.ProjectID(),
			Slug:       doc.Slug(),
			Title:      doc.Title(),
			Content:    doc.Content(),
			DocType:    doc.DocType(),
			Visibility: visibility,
			Tags:       doc.Tags(),
			Summary:    doc.Summary(),
			CreatedAt:  doc.CreatedAt(),
			UpdatedAt:  updatedAt,
		})
		if err != nil {
			return nil, err
		}
		s.docs[i] = updated
		return updated, nil
	}
	return nil, documentNotFound(projectID, slug)
}

func (s *memoryStore) GetOrCreateThread(_ context.Context, projectID string, slug string, threadKey string, title string, createdBy string, targetAnchor string, now time.Time) (*DiscussionThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.threads {
		thread := &s.threads[i]
		if thread.TargetProjectID == projectID && thread.TargetSlug == slug && thread.ThreadKey == threadKey {
			return thread, nil
		}
	}
	thread := DiscussionThread{
		ID:              s.nextThrd,
		TargetType:      TargetTypeDocument,
		TargetProjectID: projectID,
		TargetSlug:      slug,
		TargetAnchor:    targetAnchor,
		ThreadKey:       threadKey,
		Title:           title,
		Status:          ThreadStatusOpen,
		CreatedBy:       createdBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.nextThrd++
	s.threads = append(s.threads, thread)
	return &s.threads[len(s.threads)-1], nil
}

func (s *memoryStore) CreateThread(_ context.Context, thread DiscussionThread) (*DiscussionThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread.ID = s.nextThrd
	s.nextThrd++
	s.threads = append(s.threads, thread)
	return &s.threads[len(s.threads)-1], nil
}

func (s *memoryStore) GetThread(_ context.Context, id int64) (*DiscussionThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.threads {
		if s.threads[i].ID == id {
			return &s.threads[i], nil
		}
	}
	return nil, threadNotFound(id)
}

func (s *memoryStore) ListThreads(_ context.Context, query ListThreadsQuery) ([]DiscussionThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var threads []DiscussionThread
	for _, thread := range s.threads {
		if thread.TargetType != query.TargetType || thread.TargetProjectID != query.TargetProjectID || thread.TargetSlug != query.TargetSlug {
			continue
		}
		if query.Status != "" && thread.Status != query.Status {
			continue
		}
		threads = append(threads, thread)
	}
	limit := normalizeDiscussionThreadLimit(query.Limit)
	if len(threads) > limit {
		threads = threads[:limit]
	}
	return threads, nil
}

func (s *memoryStore) UpdateThread(_ context.Context, id int64, patch ThreadPatch, updatedAt time.Time) (*DiscussionThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.threads {
		thread := &s.threads[i]
		if thread.ID != id {
			continue
		}
		if patch.Status != nil {
			thread.Status = *patch.Status
		}
		if patch.Title != nil {
			thread.Title = *patch.Title
		}
		if patch.Summary != nil {
			thread.Summary = *patch.Summary
		}
		if patch.ResolutionSummary != nil {
			thread.ResolutionSummary = *patch.ResolutionSummary
		}
		if patch.HasMetadata {
			thread.MetadataJSON = cloneBytes(patch.MetadataJSON)
		}
		thread.UpdatedAt = updatedAt
		return thread, nil
	}
	return nil, threadNotFound(id)
}

func (s *memoryStore) AddComment(_ context.Context, comment DiscussionComment, now time.Time) (*DiscussionComment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	comment.ID = s.nextComm
	comment.CreatedAt = now
	comment.UpdatedAt = now
	s.nextComm++
	s.comments = append(s.comments, comment)
	for i := range s.threads {
		if s.threads[i].ID == comment.ThreadID {
			s.threads[i].LastCommentAt = &now
			s.threads[i].UpdatedAt = now
		}
	}
	return &s.comments[len(s.comments)-1], nil
}

func (s *memoryStore) GetComment(_ context.Context, id int64) (*DiscussionComment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.comments {
		if s.comments[i].ID == id {
			return &s.comments[i], nil
		}
	}
	return nil, commentNotFound(id)
}

func (s *memoryStore) ListComments(_ context.Context, threadID int64) ([]DiscussionComment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var comments []DiscussionComment
	for _, comment := range s.comments {
		if comment.ThreadID == threadID {
			comments = append(comments, comment)
		}
	}
	return comments, nil
}

func hasAllTags(have []string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	seen := map[string]bool{}
	for _, tag := range have {
		seen[tag] = true
	}
	for _, tag := range want {
		if !seen[tag] {
			return false
		}
	}
	return true
}
