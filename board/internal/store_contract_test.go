package board

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStoreSQLSerializesCommentCreationWithPostPurge(t *testing.T) {
	if !strings.Contains(strings.ToLower(lockPostForCommentSQL), "for update") {
		t.Fatal("comment creation must lock the owning post before insert")
	}
	if !strings.Contains(strings.ToLower(lockParentForCommentSQL), "for update") {
		t.Fatal("reply creation must lock the parent comment before insert")
	}
	if strings.Contains(strings.ToLower(listCommentsSQL), "with recursive") {
		t.Fatal("direct-child listing must not recursively scan descendant subtrees")
	}
	if !strings.Contains(commentPathSQL, "parent.post_id = $2") {
		t.Fatal("comment paths must remain scoped to the target post")
	}
}

func TestMemoryStoreRejectsReplyToPurgedParent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	post := mustStorePost(t, store)
	parent := mustStoreComment(t, store, post.ID, nil)
	purgeCtx := withPurgeAudit(WithAuthenticatedAdapterIdentity(ctx, "test-adapter"), PurgeAudit{AdapterIdentity: "test-adapter", Reason: "test"})
	if err := store.PurgeComment(purgeCtx, parent.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, err := store.CreateComment(ctx, &Comment{PostID: post.ID, ParentCommentID: &parent.ID, BodyMarkdown: "reply", AuthorIdentity: "agent", Status: CommentStatusActive})
	if !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("reply to purged parent error = %v", err)
	}
}

func mustStorePost(t *testing.T, store *MemoryStore) *Post {
	t.Helper()
	post, err := store.CreatePost(context.Background(), &Post{ProjectID: "project-a", Title: "topic", BodyMarkdown: "body", AuthorIdentity: "agent", Status: PostStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	return post
}

func mustStoreComment(t *testing.T, store *MemoryStore, postID int64, parentID *int64) *Comment {
	t.Helper()
	comment, err := store.CreateComment(context.Background(), &Comment{PostID: postID, ParentCommentID: parentID, BodyMarkdown: "comment", AuthorIdentity: "agent", Status: CommentStatusActive})
	if err != nil {
		t.Fatal(err)
	}
	return comment
}

func TestSearchCursorSeparatesPostAndCommentIDSpaces(t *testing.T) {
	post := SearchResult{Kind: SearchResultPost, ID: 7}
	comment := SearchResult{Kind: SearchResultComment, ID: 7}
	if searchCursor(post) != 14 || searchCursor(comment) != 15 {
		t.Fatalf("search cursors = (%d, %d), want (14, 15)", searchCursor(post), searchCursor(comment))
	}
	page := searchPage([]SearchResult{post, comment, {Kind: SearchResultPost, ID: 8}}, 2)
	if page.NextAfterID == nil || *page.NextAfterID != 15 {
		t.Fatalf("next cursor = %v, want 15", page.NextAfterID)
	}
}
