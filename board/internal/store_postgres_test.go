package board

import (
	"context"
	"errors"
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
	if err := store.PurgeComment(ctx, root.ID, now.Add(time.Second)); err != nil {
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
	if err := store.PurgePost(ctx, post.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	storedPost, err := store.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedPost == nil || storedPost.Status != PostStatusDeleted || storedPost.Title != "" || storedPost.AuthorIdentity != "" {
		t.Fatalf("purged post = %#v", storedPost)
	}
	if err := store.PurgePost(ctx, post.ID, now.Add(3*time.Second)); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("second purge error = %v", err)
	}
}
