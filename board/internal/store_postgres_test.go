package board

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresPurgeAndTreeTraversal(t *testing.T) {
	databaseURL := os.Getenv("DEN_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_BOARD_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	post, err := store.CreatePost(ctx, &Post{ProjectID: "board-store-test", Title: "Purge target", BodyMarkdown: "misleading body", AuthorIdentity: "agent", Status: PostStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateComment(ctx, &Comment{PostID: post.ID, BodyMarkdown: "root", AuthorIdentity: "agent", Status: CommentStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateComment(ctx, &Comment{PostID: post.ID, ParentCommentID: &root.ID, BodyMarkdown: "child", AuthorIdentity: "agent", Status: CommentStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	purgeCtx := withPurgeAudit(WithAuthenticatedAdapterIdentity(ctx, "test-adapter"), PurgeAudit{AdapterIdentity: "test-adapter", Reason: "test"})
	if err := store.PurgeComment(purgeCtx, root.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListComments(ctx, ListCommentsQuery{PostID: post.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 1 || page.Comments[0].Status != CommentStatusDeleted || page.Comments[0].BodyMarkdown != "" {
		t.Fatalf("tombstone page = %#v", page)
	}
	path, err := store.GetCommentPath(ctx, child.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(path.Comments) != 2 || path.Comments[0].Status != CommentStatusDeleted {
		t.Fatalf("path = %#v", path)
	}
	if _, err := store.CreateComment(ctx, &Comment{PostID: post.ID, ParentCommentID: &root.ID, BodyMarkdown: "late reply", AuthorIdentity: "agent", Status: CommentStatusActive, CreatedAt: now, UpdatedAt: now}); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("reply to purged parent error = %v", err)
	}
	if err := store.PurgePost(purgeCtx, post.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	storedPost, err := store.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPost == nil || storedPost.Status != PostStatusDeleted || storedPost.Title != "" || storedPost.AuthorIdentity != "" {
		t.Fatalf("purged post = %#v", storedPost)
	}
	if err := store.PurgePost(purgeCtx, post.ID, now.Add(3*time.Second)); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("second purge error = %v", err)
	}
}

func TestStorePostgresFullTextSearchAndPurgeExclusion(t *testing.T) {
	ctx, store, closeStore := postgresTestStore(t)
	defer closeStore()
	now := time.Now().UTC()
	projectID := fmt.Sprintf("board-fts-%d", now.UnixNano())
	post, err := store.CreatePost(ctx, &Post{
		ProjectID: projectID, Title: "Alpha release", BodyMarkdown: "several intervening words before omega",
		AuthorIdentity: "agent", Status: PostStatusActive, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Search(ctx, SearchQuery{ProjectID: projectID, Query: "alpha omega", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].ID != post.ID || page.Results[0].Rank <= 0 {
		t.Fatalf("non-contiguous FTS results = %#v", page.Results)
	}
	purgeCtx := withPurgeAudit(WithAuthenticatedAdapterIdentity(ctx, "test-adapter"), PurgeAudit{AdapterIdentity: "test-adapter", Reason: "test"})
	if err := store.PurgePost(purgeCtx, post.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err = store.Search(ctx, SearchQuery{ProjectID: projectID, Query: "alpha omega", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 0 {
		t.Fatalf("purged post remained searchable: %#v", page.Results)
	}
}

func TestStorePostgresDropsDeletedOnlyTombstoneBranch(t *testing.T) {
	ctx, store, closeStore := postgresTestStore(t)
	defer closeStore()
	now := time.Now().UTC()
	post, err := store.CreatePost(ctx, &Post{
		ProjectID: fmt.Sprintf("board-tombstone-%d", now.UnixNano()), Title: "Tombstone", BodyMarkdown: "body",
		AuthorIdentity: "agent", Status: PostStatusActive, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateComment(ctx, &Comment{PostID: post.ID, BodyMarkdown: "root", AuthorIdentity: "agent", Status: CommentStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateComment(ctx, &Comment{PostID: post.ID, ParentCommentID: &root.ID, BodyMarkdown: "child", AuthorIdentity: "agent", Status: CommentStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	purgeCtx := withPurgeAudit(WithAuthenticatedAdapterIdentity(ctx, "test-adapter"), PurgeAudit{AdapterIdentity: "test-adapter", Reason: "test"})
	if err := store.PurgeComment(purgeCtx, root.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListComments(ctx, ListCommentsQuery{PostID: post.ID, Limit: 10})
	if err != nil || len(page.Comments) != 1 || page.Comments[0].ID != root.ID {
		t.Fatalf("active-descendant tombstone page = %#v, %v", page, err)
	}
	if err := store.PurgeComment(purgeCtx, child.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListComments(ctx, ListCommentsQuery{PostID: post.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 0 {
		t.Fatalf("deleted-only tombstone branch remained visible: %#v", page.Comments)
	}
}

func postgresTestStore(t *testing.T) (context.Context, *Store, func()) {
	t.Helper()
	databaseURL := os.Getenv("DEN_BOARD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_BOARD_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, NewStore(pool), pool.Close
}
