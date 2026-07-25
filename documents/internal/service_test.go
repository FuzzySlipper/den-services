package documents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceDocumentVisibilitySearchArchiveAndDelete(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, StaticGuidanceReader{Ready: true}, fixedClock())

	doc, err := service.StoreDocument(ctx, "den-services", StoreDocumentRequest{
		Slug:    "documents-contract",
		Title:   "Documents Contract",
		Content: "Postgres search archive comments",
		DocType: DocTypeSpec,
		Tags:    []string{"documents", "fts"},
		Summary: "Searchable doc",
	})
	if err != nil {
		t.Fatalf("StoreDocument() error = %v", err)
	}
	if doc.Visibility() != VisibilityNormal {
		t.Fatalf("Visibility = %q", doc.Visibility())
	}
	archived, err := service.UpdateVisibility(ctx, "den-services", "documents-contract", VisibilityArchived)
	if err != nil {
		t.Fatalf("UpdateVisibility() error = %v", err)
	}
	if archived.Visibility() != VisibilityArchived {
		t.Fatalf("archived visibility = %q", archived.Visibility())
	}
	updated, err := service.StoreDocument(ctx, "den-services", StoreDocumentRequest{
		Slug:    "documents-contract",
		Title:   "Updated Contract",
		Content: "Updated content",
		DocType: DocTypeSpec,
		Summary: "Updated summary",
	})
	if err != nil {
		t.Fatalf("StoreDocument(upsert) error = %v", err)
	}
	if updated.Visibility() != VisibilityArchived {
		t.Fatalf("upsert changed visibility to %q", updated.Visibility())
	}
	normal, err := service.ListDocuments(ctx, ListDocumentsQuery{ProjectID: "den-services"})
	if err != nil {
		t.Fatalf("ListDocuments(normal) error = %v", err)
	}
	if len(normal) != 0 {
		t.Fatalf("normal list included archived doc: %#v", normal)
	}
	archivedDocs, _, err := service.QueryArchivedDocuments(ctx, "", "den-services", "", nil)
	if err != nil {
		t.Fatalf("QueryArchivedDocuments(list) error = %v", err)
	}
	if len(archivedDocs) != 1 || archivedDocs[0].Slug != "documents-contract" {
		t.Fatalf("archived docs = %#v", archivedDocs)
	}
	results, err := service.SearchDocuments(ctx, "Updated", "den-services")
	if err != nil {
		t.Fatalf("SearchDocuments() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("normal search included archived doc: %#v", results)
	}
	_, archivedResults, err := service.QueryArchivedDocuments(ctx, "Updated", "den-services", "", nil)
	if err != nil {
		t.Fatalf("QueryArchivedDocuments(search) error = %v", err)
	}
	if len(archivedResults) != 1 {
		t.Fatalf("archived search results = %#v", archivedResults)
	}
	preflight, err := service.ArchivePreflight(ctx, "den-services", "documents-contract")
	if err != nil {
		t.Fatalf("ArchivePreflight() error = %v", err)
	}
	if !preflight.CanArchive || !preflight.GuidanceReferenceCheckReady {
		t.Fatalf("preflight = %#v", preflight)
	}
	deleted, err := service.DeleteDocument(ctx, "den-services", "documents-contract")
	if err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	if !deleted {
		t.Fatal("DeleteDocument() = false")
	}
	if _, err := service.GetDocument(ctx, "den-services", "documents-contract"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("GetDocument(deleted) error = %v", err)
	}
}

func TestServiceDocumentDiscussionReturnsAnchoredThreadWithoutDefaultThread(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, StaticGuidanceReader{Ready: true}, fixedClock())
	if _, err := service.StoreDocument(ctx, "den-services", StoreDocumentRequest{Slug: "anchored", Title: "Anchored", Content: "Body"}); err != nil {
		t.Fatalf("StoreDocument() error = %v", err)
	}
	comment, thread, err := service.CommentOnDocument(ctx, "den-services", "anchored", CommentOnDocumentRequest{
		AuthorIdentity: "reviewer",
		BodyMarkdown:   "Anchored feedback",
		Anchor:         "Scope",
	})
	if err != nil {
		t.Fatalf("CommentOnDocument(anchor) error = %v", err)
	}
	if thread.ThreadKey != "section:Scope" {
		t.Fatalf("thread key = %q", thread.ThreadKey)
	}
	detail, err := service.GetDocumentDiscussion(ctx, "den-services", "anchored", false, false, "")
	if err != nil {
		t.Fatalf("GetDocumentDiscussion() error = %v", err)
	}
	if detail.DefaultThread != nil || len(detail.Threads) != 1 || len(detail.Comments) != 1 || detail.Comments[0].ID != comment.ID {
		t.Fatalf("discussion detail = %#v", detail)
	}
}

func TestNormalizeDiscussionThreadLimit(t *testing.T) {
	if got := normalizeDiscussionThreadLimit(0); got != DefaultDiscussionThreadLimit {
		t.Fatalf("zero limit = %d, want %d", got, DefaultDiscussionThreadLimit)
	}
	if got := normalizeDiscussionThreadLimit(-1); got != DefaultDiscussionThreadLimit {
		t.Fatalf("negative limit = %d, want %d", got, DefaultDiscussionThreadLimit)
	}
	if got := normalizeDiscussionThreadLimit(7); got != 7 {
		t.Fatalf("explicit limit = %d, want 7", got)
	}
}

func TestServiceCommentOnDocumentRejectsParentFromAnotherDocument(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, StaticGuidanceReader{Ready: true}, fixedClock())
	for _, slug := range []string{"source", "target"} {
		if _, err := service.StoreDocument(ctx, "den-services", StoreDocumentRequest{Slug: slug, Title: slug, Content: "Body"}); err != nil {
			t.Fatalf("StoreDocument(%s) error = %v", slug, err)
		}
	}
	parent, _, err := service.CommentOnDocument(ctx, "den-services", "source", CommentOnDocumentRequest{AuthorIdentity: "reviewer", BodyMarkdown: "Source", Anchor: "Scope"})
	if err != nil {
		t.Fatalf("CommentOnDocument(source) error = %v", err)
	}
	if _, _, err := service.CommentOnDocument(ctx, "den-services", "target", CommentOnDocumentRequest{AuthorIdentity: "reviewer", BodyMarkdown: "Wrong target", ParentCommentID: &parent.ID}); !errors.Is(err, ErrParentDocumentMismatch) {
		t.Fatalf("cross-document parent error = %v", err)
	}
	threads, err := store.ListThreads(ctx, ListThreadsQuery{TargetType: TargetTypeDocument, TargetProjectID: "den-services", TargetSlug: "target"})
	if err != nil {
		t.Fatalf("ListThreads(target) error = %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("cross-document reply created stray target thread: %#v", threads)
	}
}

func TestServiceDiscussionGreenPathReplyAndResolve(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, StaticGuidanceReader{Ready: true}, fixedClock())
	if _, err := service.StoreDocument(ctx, "den-services", StoreDocumentRequest{Slug: "doc", Title: "Doc", Content: "Body"}); err != nil {
		t.Fatalf("StoreDocument() error = %v", err)
	}
	detail, err := service.GetDocumentDiscussion(ctx, "den-services", "doc", false, false, "")
	if err != nil {
		t.Fatalf("GetDocumentDiscussion(no create) error = %v", err)
	}
	if detail.DefaultThread != nil || len(detail.Comments) != 0 {
		t.Fatalf("no-create detail = %#v", detail)
	}
	ensured, err := service.EnsureDocumentDiscussion(ctx, "den-services", "doc")
	if err != nil {
		t.Fatalf("EnsureDocumentDiscussion() error = %v", err)
	}
	if ensured.DefaultThread == nil || ensured.DefaultThread.ThreadKey != DefaultThreadKey {
		t.Fatalf("ensured detail = %#v", ensured)
	}
	root, thread, err := service.CommentOnDocument(ctx, "den-services", "doc", CommentOnDocumentRequest{
		AuthorIdentity: "pi",
		BodyMarkdown:   "Looks good",
	})
	if err != nil {
		t.Fatalf("CommentOnDocument(root) error = %v", err)
	}
	if thread.ThreadKey != DefaultThreadKey || root.ParentCommentID != nil {
		t.Fatalf("root=%#v thread=%#v", root, thread)
	}
	reply, _, err := service.CommentOnDocument(ctx, "den-services", "doc", CommentOnDocumentRequest{
		AuthorIdentity:  "coder",
		BodyMarkdown:    "Reply",
		ParentCommentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("CommentOnDocument(reply) error = %v", err)
	}
	if reply.ParentCommentID == nil || *reply.ParentCommentID != root.ID {
		t.Fatalf("reply parent = %#v", reply.ParentCommentID)
	}
	anchorComment, anchorThread, err := service.CommentOnDocument(ctx, "den-services", "doc", CommentOnDocumentRequest{
		AuthorIdentity: "reviewer",
		BodyMarkdown:   "Anchor",
		Anchor:         "section-1",
	})
	if err != nil {
		t.Fatalf("CommentOnDocument(anchor) error = %v", err)
	}
	if anchorThread.ThreadKey != "section:section-1" || anchorComment.ThreadID == thread.ID {
		t.Fatalf("anchor thread/comment = %#v %#v", anchorThread, anchorComment)
	}
	anchoredReply, replyThread, err := service.CommentOnDocument(ctx, "den-services", "doc", CommentOnDocumentRequest{
		AuthorIdentity:  "coder",
		BodyMarkdown:    "Anchored reply",
		ParentCommentID: &anchorComment.ID,
	})
	if err != nil {
		t.Fatalf("CommentOnDocument(anchored reply) error = %v", err)
	}
	if replyThread.ID != anchorThread.ID || anchoredReply.ThreadID != anchorThread.ID || anchoredReply.ParentCommentID == nil || *anchoredReply.ParentCommentID != anchorComment.ID {
		t.Fatalf("anchored reply/thread = %#v %#v", anchoredReply, replyThread)
	}
	if _, err := service.CreateDiscussionComment(ctx, anchorThread.ID, CreateCommentRequest{AuthorIdentity: "bad", BodyMarkdown: "bad", ParentCommentID: &root.ID}); !errors.Is(err, ErrParentThreadMismatch) {
		t.Fatalf("cross-thread parent error = %v", err)
	}
	status := ThreadStatusResolved
	resolution := "Done"
	updated, err := service.UpdateDiscussionThread(ctx, thread.ID, UpdateThreadRequest{Status: &status, ResolutionSummary: &resolution})
	if err != nil {
		t.Fatalf("UpdateDiscussionThread() error = %v", err)
	}
	if updated.Status != ThreadStatusResolved || updated.ResolutionSummary != "Done" {
		t.Fatalf("updated thread = %#v", updated)
	}
	readThread, comments, err := service.GetDiscussionThread(ctx, thread.ID, true)
	if err != nil {
		t.Fatalf("GetDiscussionThread() error = %v", err)
	}
	if readThread.ID != thread.ID || len(comments) != 2 {
		t.Fatalf("thread/comments = %#v %#v", readThread, comments)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	}
}
