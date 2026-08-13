package board

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a deterministic store for focused service and handler tests.
// Production uses Store, whose SQL enforces the same visibility contract.
type MemoryStore struct {
	mu       sync.RWMutex
	nextID   int64
	posts    map[int64]*Post
	comments map[int64]*Comment
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1, posts: make(map[int64]*Post), comments: make(map[int64]*Comment)}
}

func (s *MemoryStore) Ping(context.Context) error { return nil }

func (s *MemoryStore) CreatePost(_ context.Context, post *Post) (*Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyPost := clonePost(post)
	copyPost.ID = s.allocateID()
	s.posts[copyPost.ID] = copyPost
	return clonePost(copyPost), nil
}

func (s *MemoryStore) GetPost(_ context.Context, id int64) (*Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePost(s.posts[id]), nil
}

func (s *MemoryStore) ListPosts(_ context.Context, query ListPostsQuery) (PostPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	posts := make([]PostSummary, 0)
	for _, post := range s.posts {
		if post.ProjectID == query.ProjectID && post.Status == PostStatusActive && post.ID > query.AfterID {
			posts = append(posts, PostSummary{ID: post.ID, ProjectID: post.ProjectID, Title: post.Title, AuthorIdentity: post.AuthorIdentity, CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt})
		}
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].ID < posts[j].ID })
	if len(posts) > query.Limit+1 {
		posts = posts[:query.Limit+1]
	}
	return postPage(posts, query.Limit), nil
}

func (s *MemoryStore) Search(_ context.Context, query SearchQuery) (SearchPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	terms := strings.Fields(strings.ToLower(query.Query))
	results := make([]SearchResult, 0)
	for _, post := range s.posts {
		if post.ProjectID != query.ProjectID || post.Status != PostStatusActive {
			continue
		}
		if containsAllTerms(post.Title+" "+post.BodyMarkdown, terms) {
			result := SearchResult{Kind: SearchResultPost, ID: post.ID, PostID: post.ID, ProjectID: post.ProjectID, Title: post.Title, AuthorIdentity: post.AuthorIdentity, Snippet: snippet(post.BodyMarkdown), CreatedAt: post.CreatedAt}
			if searchCursor(result) > query.AfterID {
				results = append(results, result)
			}
		}
		for _, comment := range s.comments {
			if comment.PostID == post.ID && comment.Status == CommentStatusActive && containsAllTerms(comment.BodyMarkdown, terms) {
				result := SearchResult{Kind: SearchResultComment, ID: comment.ID, PostID: post.ID, ProjectID: post.ProjectID, Title: post.Title, AuthorIdentity: comment.AuthorIdentity, Snippet: snippet(comment.BodyMarkdown), CreatedAt: comment.CreatedAt}
				if searchCursor(result) > query.AfterID {
					results = append(results, result)
				}
			}
		}
	}
	sort.Slice(results, func(i, j int) bool { return searchCursor(results[i]) < searchCursor(results[j]) })
	if len(results) > query.Limit+1 {
		results = results[:query.Limit+1]
	}
	return searchPage(results, query.Limit), nil
}

func containsAllTerms(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return len(terms) > 0
}

func (s *MemoryStore) CreateComment(_ context.Context, comment *Comment) (*Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	post := s.posts[comment.PostID]
	if post == nil || post.Status != PostStatusActive {
		return nil, postNotFound()
	}
	if comment.ParentCommentID != nil {
		parent := s.comments[*comment.ParentCommentID]
		if parent == nil || parent.Status != CommentStatusActive {
			return nil, commentNotFound()
		}
		if parent.PostID != comment.PostID {
			return nil, validationFailed(ErrParentPostMismatch)
		}
	}
	copyComment := cloneComment(comment)
	copyComment.ID = s.allocateID()
	s.comments[copyComment.ID] = copyComment
	return cloneComment(copyComment), nil
}

func (s *MemoryStore) GetComment(_ context.Context, id int64) (*Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneComment(s.comments[id]), nil
}

func (s *MemoryStore) ListComments(_ context.Context, query ListCommentsQuery) (CommentPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	comments := make([]Comment, 0)
	for _, comment := range s.comments {
		if comment.PostID != query.PostID || comment.ID <= query.AfterID || !sameParent(comment.ParentCommentID, query.ParentCommentID) {
			continue
		}
		if comment.Status == CommentStatusActive || s.hasActiveDescendant(comment.ID) {
			comments = append(comments, *cloneComment(comment))
		}
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	if len(comments) > query.Limit+1 {
		comments = comments[:query.Limit+1]
	}
	return commentPage(comments, query.Limit), nil
}

func (s *MemoryStore) GetCommentPath(_ context.Context, id int64, limit int) (CommentPath, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	target := s.comments[id]
	if target == nil {
		return CommentPath{}, nil
	}
	comments := make([]Comment, 0, limit)
	seen := make(map[int64]struct{})
	current := target
	for current != nil && len(comments) < limit {
		if _, exists := seen[current.ID]; exists {
			return CommentPath{}, ErrCommentCycle
		}
		seen[current.ID] = struct{}{}
		comments = append(comments, *cloneComment(current))
		if current.ParentCommentID == nil {
			current = nil
		} else {
			current = s.comments[*current.ParentCommentID]
		}
	}
	truncated := current != nil
	for left, right := 0, len(comments)-1; left < right; left, right = left+1, right-1 {
		comments[left], comments[right] = comments[right], comments[left]
	}
	return CommentPath{Post: clonePost(s.posts[target.PostID]), Comments: comments, Truncated: truncated}, nil
}

func (s *MemoryStore) PurgePost(ctx context.Context, id int64, now time.Time) error {
	if err := requirePurgeAudit(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	post := s.posts[id]
	if post == nil || post.Status != PostStatusActive {
		return postNotFound()
	}
	scrubPost(post, now)
	for _, comment := range s.comments {
		if comment.PostID == id && comment.Status == CommentStatusActive {
			scrubComment(comment, now)
		}
	}
	return nil
}

func (s *MemoryStore) PurgeComment(ctx context.Context, id int64, now time.Time) error {
	if err := requirePurgeAudit(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	comment := s.comments[id]
	if comment == nil || comment.Status != CommentStatusActive {
		return commentNotFound()
	}
	scrubComment(comment, now)
	return nil
}

func (s *MemoryStore) allocateID() int64 { id := s.nextID; s.nextID++; return id }

func (s *MemoryStore) hasActiveDescendant(parentID int64) bool {
	for _, comment := range s.comments {
		if comment.ParentCommentID == nil || *comment.ParentCommentID != parentID {
			continue
		}
		if comment.Status == CommentStatusActive || s.hasActiveDescendant(comment.ID) {
			return true
		}
	}
	return false
}

func sameParent(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func scrubPost(post *Post, now time.Time) {
	post.Title, post.BodyMarkdown, post.AuthorIdentity, post.MetadataJSON = "", "", "", nil
	post.Status, post.UpdatedAt, post.DeletedAt = PostStatusDeleted, now, &now
}

func scrubComment(comment *Comment, now time.Time) {
	comment.BodyMarkdown, comment.AuthorIdentity, comment.MetadataJSON = "", "", nil
	comment.Status, comment.UpdatedAt, comment.DeletedAt = CommentStatusDeleted, now, &now
}

func clonePost(post *Post) *Post {
	if post == nil {
		return nil
	}
	copyPost := *post
	copyPost.MetadataJSON = cloneBytes(post.MetadataJSON)
	return &copyPost
}

func cloneComment(comment *Comment) *Comment {
	if comment == nil {
		return nil
	}
	copyComment := *comment
	copyComment.ParentCommentID = cloneInt64Pointer(comment.ParentCommentID)
	copyComment.MetadataJSON = cloneBytes(comment.MetadataJSON)
	return &copyComment
}

func snippet(value string) string {
	const maximum = 240
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
