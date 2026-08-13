package board

import (
	"context"
	"strings"
	"time"
)

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type BoardStore interface {
	Ping(ctx context.Context) error
	CreatePost(ctx context.Context, post *Post) (*Post, error)
	GetPost(ctx context.Context, id int64) (*Post, error)
	ListPosts(ctx context.Context, query ListPostsQuery) (PostPage, error)
	Search(ctx context.Context, query SearchQuery) (SearchPage, error)
	CreateComment(ctx context.Context, comment *Comment) (*Comment, error)
	GetComment(ctx context.Context, id int64) (*Comment, error)
	ListComments(ctx context.Context, query ListCommentsQuery) (CommentPage, error)
	GetCommentPath(ctx context.Context, id int64, limit int) (CommentPath, error)
	PurgePost(ctx context.Context, id int64, now time.Time) error
	PurgeComment(ctx context.Context, id int64, now time.Time) error
}

type Service struct {
	store    BoardStore
	projects ProjectValidator
	clock    func() time.Time
	limits   Limits
}

func NewService(store BoardStore, projects ProjectValidator, clock func() time.Time) *Service {
	return NewServiceWithLimits(store, projects, clock, DefaultLimits())
}

func NewServiceWithLimits(store BoardStore, projects ProjectValidator, clock func() time.Time, limits Limits) *Service {
	if projects == nil {
		projects = NoopProjectValidator{}
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		store:    store,
		projects: projects,
		clock:    clock,
		limits:   limits,
	}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreatePost(ctx context.Context, projectID string, req CreatePostRequest) (*Post, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	post, err := NewPost(Post{
		ProjectID:      projectID,
		Title:          req.Title,
		BodyMarkdown:   req.BodyMarkdown,
		AuthorIdentity: req.AuthorIdentity,
		MetadataJSON:   req.Metadata,
		Status:         PostStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreatePost(ctx, post)
}

func (s *Service) ListPosts(ctx context.Context, projectID string, query ListPostsQuery) (PostPage, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return PostPage{}, validationFailed(ErrMissingProjectID)
	}
	if err := validateAfterID(query.AfterID); err != nil {
		return PostPage{}, validationFailed(err)
	}
	limit, err := s.pageLimit(query.Limit)
	if err != nil {
		return PostPage{}, validationFailed(err)
	}
	query.ProjectID = projectID
	query.Limit = limit
	return s.store.ListPosts(ctx, query)
}

func (s *Service) Search(ctx context.Context, projectID string, query SearchQuery) (SearchPage, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return SearchPage{}, validationFailed(ErrMissingProjectID)
	}
	if strings.TrimSpace(query.Query) == "" {
		return SearchPage{}, validationFailed(ErrInvalidSearchQuery)
	}
	if err := validateAfterID(query.AfterID); err != nil {
		return SearchPage{}, validationFailed(err)
	}
	limit, err := s.pageLimit(query.Limit)
	if err != nil {
		return SearchPage{}, validationFailed(err)
	}
	query.ProjectID = projectID
	query.Query = strings.TrimSpace(query.Query)
	query.Limit = limit
	return s.store.Search(ctx, query)
}

func (s *Service) GetPost(ctx context.Context, id int64) (*Post, error) {
	if err := validateID(id); err != nil {
		return nil, validationFailed(err)
	}
	post, err := s.store.GetPost(ctx, id)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Status != PostStatusActive {
		return nil, postNotFound()
	}
	return post, nil
}

func (s *Service) CreateComment(ctx context.Context, postID int64, req CreateCommentRequest) (*Comment, error) {
	if err := validateID(postID); err != nil {
		return nil, validationFailed(err)
	}
	post, err := s.store.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Status != PostStatusActive {
		return nil, postNotFound()
	}
	if err := s.projects.AssertWritable(ctx, post.ProjectID); err != nil {
		return nil, err
	}
	parentID := cloneInt64Pointer(req.ParentCommentID)
	if parentID != nil {
		if err := validateID(*parentID); err != nil {
			return nil, validationFailed(err)
		}
		parent, err := s.store.GetComment(ctx, *parentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, commentNotFound()
		}
		if parent.Status != CommentStatusActive {
			return nil, commentNotFound()
		}
		if parent.PostID != postID {
			return nil, validationFailed(ErrParentPostMismatch)
		}
	}
	now := s.clock().UTC()
	comment, err := NewComment(Comment{
		PostID:          postID,
		ParentCommentID: parentID,
		BodyMarkdown:    req.BodyMarkdown,
		AuthorIdentity:  req.AuthorIdentity,
		MetadataJSON:    req.Metadata,
		Status:          CommentStatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreateComment(ctx, comment)
}

func (s *Service) ListComments(ctx context.Context, postID int64, query ListCommentsQuery) (CommentPage, error) {
	if err := validateID(postID); err != nil {
		return CommentPage{}, validationFailed(err)
	}
	post, err := s.store.GetPost(ctx, postID)
	if err != nil {
		return CommentPage{}, err
	}
	if post == nil || post.Status != PostStatusActive {
		return CommentPage{}, postNotFound()
	}
	if err := validateAfterID(query.AfterID); err != nil {
		return CommentPage{}, validationFailed(err)
	}
	if query.ParentCommentID != nil {
		if err := validateID(*query.ParentCommentID); err != nil {
			return CommentPage{}, validationFailed(err)
		}
		parent, err := s.store.GetComment(ctx, *query.ParentCommentID)
		if err != nil {
			return CommentPage{}, err
		}
		if parent == nil {
			return CommentPage{}, commentNotFound()
		}
		if parent.PostID != postID {
			return CommentPage{}, validationFailed(ErrParentPostMismatch)
		}
	}
	limit, err := s.pageLimit(query.Limit)
	if err != nil {
		return CommentPage{}, validationFailed(err)
	}
	query.PostID = postID
	query.Limit = limit
	return s.store.ListComments(ctx, query)
}

func (s *Service) GetComment(ctx context.Context, id int64) (*Comment, error) {
	if err := validateID(id); err != nil {
		return nil, validationFailed(err)
	}
	comment, err := s.store.GetComment(ctx, id)
	if err != nil {
		return nil, err
	}
	if comment == nil || comment.Status != CommentStatusActive {
		return nil, commentNotFound()
	}
	post, err := s.store.GetPost(ctx, comment.PostID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Status != PostStatusActive {
		return nil, commentNotFound()
	}
	return comment, nil
}

func (s *Service) GetCommentPath(ctx context.Context, id int64, limit int) (CommentPath, error) {
	if err := validateID(id); err != nil {
		return CommentPath{}, validationFailed(err)
	}
	pathLimit, err := s.pathLimit(limit)
	if err != nil {
		return CommentPath{}, validationFailed(err)
	}
	path, err := s.store.GetCommentPath(ctx, id, pathLimit)
	if err != nil {
		return CommentPath{}, err
	}
	if path.Post == nil || path.Post.Status != PostStatusActive {
		return CommentPath{}, commentNotFound()
	}
	if len(path.Comments) == 0 || path.Comments[len(path.Comments)-1].ID != id {
		return CommentPath{}, commentNotFound()
	}
	if path.Comments[len(path.Comments)-1].Status != CommentStatusActive {
		return CommentPath{}, commentNotFound()
	}
	for i := range path.Comments {
		if path.Comments[i].Status == CommentStatusDeleted {
			path.Comments[i].AuthorIdentity = ""
			path.Comments[i].BodyMarkdown = ""
			path.Comments[i].MetadataJSON = nil
			path.Comments[i].DeletedAt = nil
		}
	}
	return path, nil
}

func (s *Service) PurgePost(ctx context.Context, id int64, req PurgeRequest) error {
	if err := validateID(id); err != nil {
		return validationFailed(err)
	}
	if err := req.Validate(); err != nil {
		return validationFailed(err)
	}
	post, err := s.store.GetPost(ctx, id)
	if err != nil {
		return err
	}
	if post == nil || post.Status != PostStatusActive {
		return postNotFound()
	}
	if err := s.projects.AssertWritable(ctx, post.ProjectID); err != nil {
		return err
	}
	return s.store.PurgePost(ctx, id, s.clock().UTC())
}

func (s *Service) PurgeComment(ctx context.Context, id int64, req PurgeRequest) error {
	if err := validateID(id); err != nil {
		return validationFailed(err)
	}
	if err := req.Validate(); err != nil {
		return validationFailed(err)
	}
	comment, err := s.store.GetComment(ctx, id)
	if err != nil {
		return err
	}
	if comment == nil || comment.Status != CommentStatusActive {
		return commentNotFound()
	}
	post, err := s.store.GetPost(ctx, comment.PostID)
	if err != nil {
		return err
	}
	if post == nil || post.Status != PostStatusActive {
		return commentNotFound()
	}
	if err := s.projects.AssertWritable(ctx, post.ProjectID); err != nil {
		return err
	}
	return s.store.PurgeComment(ctx, id, s.clock().UTC())
}

func (s *Service) pageLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, ErrInvalidLimit
	}
	if limit == 0 {
		return s.limits.DefaultPageSize, nil
	}
	if limit > s.limits.MaxPageSize {
		return s.limits.MaxPageSize, nil
	}
	return limit, nil
}

func (s *Service) pathLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, ErrInvalidPathLimit
	}
	if limit == 0 {
		return s.limits.MaxPathComments, nil
	}
	if limit > s.limits.MaxPathComments {
		return s.limits.MaxPathComments, nil
	}
	return limit, nil
}
