package board

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceWalksImmediateChildrenAndPreservesPurgedStructure(t *testing.T) {
	ctx := context.Background()
	clock := fixedClock()
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, clock)
	post := createTestPost(t, service, "project-a", "Root")
	root := createTestComment(t, service, post.ID, nil, "root")
	child := createTestComment(t, service, post.ID, &root.ID, "child")
	grandchild := createTestComment(t, service, post.ID, &child.ID, "grandchild")

	roots, err := service.ListComments(ctx, post.ID, ListCommentsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots.Comments) != 1 || roots.Comments[0].ID != root.ID {
		t.Fatalf("root page = %#v", roots.Comments)
	}
	children, err := service.ListComments(ctx, post.ID, ListCommentsQuery{ParentCommentID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(children.Comments) != 1 || children.Comments[0].ID != child.ID {
		t.Fatalf("child page = %#v", children.Comments)
	}

	purgeCtx := WithAuthenticatedAdapterIdentity(ctx, "den-web-adapter")
	if err := service.PurgeComment(purgeCtx, child.ID, PurgeRequest{ActorIdentity: "caller-controlled", Reason: "misleading"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetComment(ctx, child.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("GetComment(purged) error = %v", err)
	}
	path, err := service.GetCommentPath(ctx, grandchild.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(path.Comments) != 3 || path.Comments[1].ID != child.ID || path.Comments[1].Status != CommentStatusDeleted {
		t.Fatalf("path = %#v", path.Comments)
	}
	if path.Comments[1].BodyMarkdown != "" || path.Comments[1].AuthorIdentity != "" || path.Comments[1].MetadataJSON != nil {
		t.Fatalf("purged ancestor leaked authored content: %#v", path.Comments[1])
	}
	children, err = service.ListComments(ctx, post.ID, ListCommentsQuery{ParentCommentID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(children.Comments) != 1 || children.Comments[0].Status != CommentStatusDeleted {
		t.Fatalf("structural tombstone = %#v", children.Comments)
	}
	grandchildren, err := service.ListComments(ctx, post.ID, ListCommentsQuery{ParentCommentID: &child.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(grandchildren.Comments) != 1 || grandchildren.Comments[0].ID != grandchild.ID {
		t.Fatalf("descendants through tombstone = %#v", grandchildren.Comments)
	}
}

func TestServicePurgingPostRemovesEveryNormalSurface(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, fixedClock())
	post := createTestPost(t, service, "project-a", "Misleading claim")
	comment := createTestComment(t, service, post.ID, nil, "also misleading")

	before, err := service.Search(ctx, "project-a", SearchQuery{Query: "misleading"})
	if err != nil || len(before.Results) != 2 {
		t.Fatalf("search before purge = %#v, %v", before, err)
	}
	purgeCtx := WithAuthenticatedAdapterIdentity(ctx, "den-web-adapter")
	if err := service.PurgePost(purgeCtx, post.ID, PurgeRequest{ActorIdentity: "caller-controlled", Reason: "misleading"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetPost(ctx, post.ID); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("GetPost error = %v", err)
	}
	if _, err := service.GetComment(ctx, comment.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("GetComment error = %v", err)
	}
	posts, err := service.ListPosts(ctx, "project-a", ListPostsQuery{})
	if err != nil || len(posts.Posts) != 0 {
		t.Fatalf("posts after purge = %#v, %v", posts, err)
	}
	after, err := service.Search(ctx, "project-a", SearchQuery{Query: "misleading"})
	if err != nil || len(after.Results) != 0 {
		t.Fatalf("search after purge = %#v, %v", after, err)
	}
	if _, err := service.ListComments(ctx, post.ID, ListCommentsQuery{}); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("ListComments error = %v", err)
	}
}

func TestServiceRejectsParentFromAnotherPost(t *testing.T) {
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, fixedClock())
	first := createTestPost(t, service, "project-a", "First")
	second := createTestPost(t, service, "project-a", "Second")
	parent := createTestComment(t, service, first.ID, nil, "parent")
	_, err := service.CreateComment(context.Background(), second.ID, CreateCommentRequest{ParentCommentID: &parent.ID, BodyMarkdown: "wrong tree", AuthorIdentity: "agent"})
	if !errors.Is(err, ErrParentPostMismatch) {
		t.Fatalf("CreateComment error = %v", err)
	}
}

func TestServiceRequiresAuthenticatedAdapterAndIgnoresCallerActor(t *testing.T) {
	store := &auditCaptureStore{MemoryStore: NewMemoryStore()}
	service := NewService(store, NoopProjectValidator{}, fixedClock())
	post := createTestPost(t, service, "project-a", "Topic")

	if err := service.PurgePost(context.Background(), post.ID, PurgeRequest{Reason: "misleading"}); !errors.Is(err, ErrMissingAdapterIdentity) {
		t.Fatalf("purge without adapter identity error = %v", err)
	}
	ctx := WithAuthenticatedAdapterIdentity(context.Background(), "server-den-web-adapter")
	if err := service.PurgePost(ctx, post.ID, PurgeRequest{ActorIdentity: "attacker", Reason: "misleading"}); err != nil {
		t.Fatal(err)
	}
	if store.audit.AdapterIdentity != "server-den-web-adapter" || store.audit.Reason != "misleading" {
		t.Fatalf("purge audit = %#v", store.audit)
	}
}

func TestServiceRejectsConfiguredFieldSizes(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxTitleBytes = 4
	limits.MaxBodyBytes = 5
	limits.MaxAuthorIdentityBytes = 5
	limits.MaxMetadataBytes = 4
	limits.MaxSearchQueryBytes = 4
	limits.MaxPurgeReasonBytes = 4
	service := NewServiceWithLimits(NewMemoryStore(), NoopProjectValidator{}, fixedClock(), limits)

	if _, err := service.CreatePost(context.Background(), "project-a", CreatePostRequest{Title: "title", BodyMarkdown: "body", AuthorIdentity: "agent"}); err == nil {
		t.Fatal("CreatePost accepted an oversized title")
	}
	post := createTestPostWithLimits(t, service, "p", "body", "agent")
	if _, err := service.CreateComment(context.Background(), post.ID, CreateCommentRequest{BodyMarkdown: "sixsix", AuthorIdentity: "agent"}); err == nil {
		t.Fatal("CreateComment accepted an oversized body")
	}
	if _, err := service.CreatePost(context.Background(), "p", CreatePostRequest{Title: "ok", BodyMarkdown: "body", AuthorIdentity: "agents"}); err == nil {
		t.Fatal("CreatePost accepted an oversized author identity")
	}
	if _, err := service.CreatePost(context.Background(), "p", CreatePostRequest{Title: "ok", BodyMarkdown: "body", AuthorIdentity: "agent", Metadata: []byte("12345")}); err == nil {
		t.Fatal("CreatePost accepted oversized metadata")
	}
	if _, err := service.CreateComment(context.Background(), post.ID, CreateCommentRequest{BodyMarkdown: "body", AuthorIdentity: "agents"}); err == nil {
		t.Fatal("CreateComment accepted an oversized author identity")
	}
	if _, err := service.CreateComment(context.Background(), post.ID, CreateCommentRequest{BodyMarkdown: "body", AuthorIdentity: "agent", Metadata: []byte("12345")}); err == nil {
		t.Fatal("CreateComment accepted oversized metadata")
	}
	if _, err := service.Search(context.Background(), "p", SearchQuery{Query: "query"}); err == nil {
		t.Fatal("Search accepted an oversized query")
	}
	if err := service.PurgePost(WithAuthenticatedAdapterIdentity(context.Background(), "den-web-adapter"), post.ID, PurgeRequest{Reason: "12345"}); err == nil {
		t.Fatal("PurgePost accepted an oversized reason")
	}
	projectLimits := limits
	projectLimits.MaxProjectIDBytes = 4
	projectService := NewServiceWithLimits(NewMemoryStore(), NoopProjectValidator{}, fixedClock(), projectLimits)
	if _, err := projectService.CreatePost(context.Background(), "long-project", CreatePostRequest{Title: "ok", BodyMarkdown: "body", AuthorIdentity: "agent"}); err == nil {
		t.Fatal("CreatePost accepted an oversized project id")
	}
}

func TestServiceUsesExclusiveBoundedCursors(t *testing.T) {
	service := NewServiceWithLimits(NewMemoryStore(), NoopProjectValidator{}, fixedClock(), Limits{DefaultPageSize: 2, MaxPageSize: 2, MaxPathComments: 10})
	first := createTestPost(t, service, "project-a", "First")
	second := createTestPost(t, service, "project-a", "Second")
	third := createTestPost(t, service, "project-a", "Third")
	page, err := service.ListPosts(context.Background(), "project-a", ListPostsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 2 || page.Posts[0].ID != first.ID || page.Posts[1].ID != second.ID || page.NextAfterID == nil || *page.NextAfterID != second.ID {
		t.Fatalf("first page = %#v", page)
	}
	next, err := service.ListPosts(context.Background(), "project-a", ListPostsQuery{AfterID: *page.NextAfterID})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Posts) != 1 || next.Posts[0].ID != third.ID {
		t.Fatalf("next page = %#v", next)
	}
}

func TestCreateWritesRespectOpaqueIdempotencyKeys(t *testing.T) {
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, fixedClock())
	postRequest := CreatePostRequest{Title: "Topic", BodyMarkdown: "body", AuthorIdentity: "agent", IdempotencyKey: "relay:post:1"}
	firstPost, err := service.CreatePost(context.Background(), "project-a", postRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondPost, err := service.CreatePost(context.Background(), "project-a", postRequest)
	if err != nil {
		t.Fatal(err)
	}
	if firstPost.ID != secondPost.ID {
		t.Fatalf("idempotent posts = %d, %d", firstPost.ID, secondPost.ID)
	}
	commentRequest := CreateCommentRequest{BodyMarkdown: "reply", AuthorIdentity: "agent", IdempotencyKey: "relay:comment:1"}
	firstComment, err := service.CreateComment(context.Background(), firstPost.ID, commentRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondComment, err := service.CreateComment(context.Background(), firstPost.ID, commentRequest)
	if err != nil {
		t.Fatal(err)
	}
	if firstComment.ID != secondComment.ID {
		t.Fatalf("idempotent comments = %d, %d", firstComment.ID, secondComment.ID)
	}
}

func createTestPost(t *testing.T, service *Service, projectID, title string) *Post {
	t.Helper()
	post, err := service.CreatePost(context.Background(), projectID, CreatePostRequest{Title: title, BodyMarkdown: title + " body", AuthorIdentity: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	return post
}

func createTestComment(t *testing.T, service *Service, postID int64, parentID *int64, body string) *Comment {
	t.Helper()
	comment, err := service.CreateComment(context.Background(), postID, CreateCommentRequest{ParentCommentID: parentID, BodyMarkdown: body, AuthorIdentity: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	return comment
}

func createTestPostWithLimits(t *testing.T, service *Service, projectID, body, author string) *Post {
	t.Helper()
	post, err := service.CreatePost(context.Background(), projectID, CreatePostRequest{Title: "p", BodyMarkdown: body, AuthorIdentity: author})
	if err != nil {
		t.Fatal(err)
	}
	return post
}

type auditCaptureStore struct {
	*MemoryStore
	audit PurgeAudit
}

func (s *auditCaptureStore) PurgePost(ctx context.Context, id int64, now time.Time) error {
	audit, ok := purgeAuditFromContext(ctx)
	if !ok {
		return ErrMissingAdapterIdentity
	}
	s.audit = audit
	return s.MemoryStore.PurgePost(ctx, id, now)
}

func (s *auditCaptureStore) PurgeComment(ctx context.Context, id int64, now time.Time) error {
	audit, ok := purgeAuditFromContext(ctx)
	if !ok {
		return ErrMissingAdapterIdentity
	}
	s.audit = audit
	return s.MemoryStore.PurgeComment(ctx, id, now)
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, time.August, 13, 2, 0, 0, 0, time.UTC) }
}
