package boardrelay

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestSyncImportsExternalIssueAndCommentVerbatimWithoutReplay(t *testing.T) {
	now := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	board := newFakeBoard()
	github := newFakeGitHub(
		GitHubIssue{ID: 100, Number: 7, Title: "Lengthy external thinking", Body: "Unchanged **Markdown**\n\n" + `<!-- den-board-relay:v1 project="alpha" -->`, Login: "remote-agent", HTMLURL: "https://example.test/issues/7", CreatedAt: now, UpdatedAt: now},
	)
	github.comments[7] = []GitHubComment{{ID: 200, Body: "A sprawling reply stays exactly as supplied.", Login: "remote-agent", HTMLURL: "https://example.test/issues/7#issuecomment-200", CreatedAt: now, UpdatedAt: now}}
	service := mustService(t, newFakeStore(), board, github, now)

	first, err := service.Sync(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if first.ImportedPosts != 1 || first.ImportedComments != 1 {
		t.Fatalf("first receipt = %#v", first)
	}
	if len(board.posts) != 1 || board.posts[0].BodyMarkdown != github.issues[0].Body {
		t.Fatalf("post was not imported verbatim: %#v", board.posts)
	}
	if len(board.comments) != 1 || board.comments[0].BodyMarkdown != github.comments[7][0].Body {
		t.Fatalf("comment was not imported verbatim: %#v", board.comments)
	}

	second, err := service.Sync(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if second.ImportedPosts != 0 || second.ImportedComments != 0 || len(board.posts) != 1 || len(board.comments) != 1 {
		t.Fatalf("replay receipt = %#v, posts=%d comments=%d", second, len(board.posts), len(board.comments))
	}
}

func TestSyncExportsBoardTreeOnceAndPreservesParentMarker(t *testing.T) {
	now := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	board := newFakeBoard()
	post := board.addPost("alpha", "Topic", "Board body", "local")
	parent := board.addComment(post.ID, nil, "Parent", "local")
	board.addComment(post.ID, &parent.ID, "Child", "local")
	github := newFakeGitHub()
	service := mustService(t, newFakeStore(), board, github, now)

	receipt, err := service.Sync(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ExportedPosts != 1 || receipt.ExportedComments != 2 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(github.issues) != 1 || len(github.comments[github.issues[0].Number]) != 2 {
		t.Fatalf("github state = %#v %#v", github.issues, github.comments)
	}
	if _, found := parseMarker(github.comments[github.issues[0].Number][1].Body); !found {
		t.Fatalf("child export did not include relay marker: %q", github.comments[github.issues[0].Number][1].Body)
	}

	second, err := service.Sync(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if second.ExportedPosts != 0 || second.ExportedComments != 0 || len(github.issues) != 1 || len(github.comments[github.issues[0].Number]) != 2 {
		t.Fatalf("replay receipt = %#v", second)
	}
}

func TestSetVisibilityRequiresExplicitValidValue(t *testing.T) {
	now := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	github := newFakeGitHub()
	service := mustService(t, newFakeStore(), newFakeBoard(), github, now)
	if err := service.SetVisibility(context.Background(), VisibilityRequest{Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	if github.visibility != "private" {
		t.Fatalf("visibility = %q", github.visibility)
	}
	if err := service.SetVisibility(context.Background(), VisibilityRequest{Visibility: "auto"}); err == nil {
		t.Fatal("SetVisibility accepted auto")
	}
}

func mustService(t *testing.T, store *fakeStore, board *fakeBoard, github *fakeGitHub, now time.Time) *Service {
	t.Helper()
	service, err := NewService(store, board, github, "owner/relay", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type fakeStore struct{ mappings []Mapping }

func newFakeStore() *fakeStore                  { return &fakeStore{} }
func (s *fakeStore) Ping(context.Context) error { return nil }
func (s *fakeStore) FindByBoard(_ context.Context, project string, kind string, id int64) (*Mapping, error) {
	for index := range s.mappings {
		value := &s.mappings[index]
		if value.ProjectID == project && value.BoardKind == kind && value.BoardID == id {
			copy := *value
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) FindByGitHub(_ context.Context, project string, kind string, id int64) (*Mapping, error) {
	for index := range s.mappings {
		value := &s.mappings[index]
		if value.ProjectID == project && value.GitHubKind == kind && value.GitHubID == id {
			copy := *value
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) Save(_ context.Context, mapping Mapping) error {
	for index := range s.mappings {
		if s.mappings[index].ProjectID == mapping.ProjectID && s.mappings[index].GitHubKind == mapping.GitHubKind && s.mappings[index].GitHubID == mapping.GitHubID {
			s.mappings[index] = mapping
			return nil
		}
	}
	s.mappings = append(s.mappings, mapping)
	return nil
}

type fakeBoard struct {
	posts       []BoardPost
	comments    []BoardComment
	postKeys    map[string]int64
	commentKeys map[string]int64
	nextID      int64
}

func newFakeBoard() *fakeBoard {
	return &fakeBoard{postKeys: map[string]int64{}, commentKeys: map[string]int64{}, nextID: 1}
}

func (b *fakeBoard) addPost(project, title, body, author string) BoardPost {
	post := BoardPost{ID: b.nextID, ProjectID: project, Title: title, BodyMarkdown: body, AuthorIdentity: author, Status: "active"}
	b.nextID++
	b.posts = append(b.posts, post)
	return post
}

func (b *fakeBoard) addComment(postID int64, parent *int64, body, author string) BoardComment {
	comment := BoardComment{ID: b.nextID, PostID: postID, ParentCommentID: parent, BodyMarkdown: body, AuthorIdentity: author, Status: "active"}
	b.nextID++
	b.comments = append(b.comments, comment)
	return comment
}

func (b *fakeBoard) ListPosts(_ context.Context, project string, after *int64, limit int) (BoardPage, error) {
	var posts []BoardPost
	for _, post := range b.posts {
		if post.ProjectID == project && (after == nil || post.ID > *after) {
			posts = append(posts, post)
		}
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].ID < posts[j].ID })
	return BoardPage{Posts: posts}, nil
}

func (b *fakeBoard) GetPost(_ context.Context, id int64) (*BoardPost, error) {
	for _, post := range b.posts {
		if post.ID == id {
			copy := post
			return &copy, nil
		}
	}
	return nil, nil
}

func (b *fakeBoard) GetComment(_ context.Context, id int64) (*BoardComment, error) {
	for _, comment := range b.comments {
		if comment.ID == id {
			copy := comment
			return &copy, nil
		}
	}
	return nil, nil
}

func (b *fakeBoard) ListComments(_ context.Context, postID int64, parent *int64, after *int64, limit int) (CommentPage, error) {
	var comments []BoardComment
	for _, comment := range b.comments {
		if comment.PostID == postID && sameID(comment.ParentCommentID, parent) && (after == nil || comment.ID > *after) {
			comments = append(comments, comment)
		}
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	return CommentPage{Comments: comments}, nil
}

func (b *fakeBoard) CreatePost(_ context.Context, project string, request BoardCreatePostRequest) (*BoardPost, error) {
	if id := b.postKeys[request.IdempotencyKey]; id > 0 {
		return b.GetPost(context.Background(), id)
	}
	post := b.addPost(project, request.Title, request.BodyMarkdown, request.AuthorIdentity)
	b.postKeys[request.IdempotencyKey] = post.ID
	return &post, nil
}

func (b *fakeBoard) CreateComment(_ context.Context, postID int64, request BoardCreateCommentRequest) (*BoardComment, error) {
	if id := b.commentKeys[request.IdempotencyKey]; id > 0 {
		return b.GetComment(context.Background(), id)
	}
	comment := b.addComment(postID, request.ParentCommentID, request.BodyMarkdown, request.AuthorIdentity)
	b.commentKeys[request.IdempotencyKey] = comment.ID
	return &comment, nil
}

func sameID(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

type fakeGitHub struct {
	issues     []GitHubIssue
	comments   map[int64][]GitHubComment
	visibility string
	nextID     int64
}

func newFakeGitHub(issues ...GitHubIssue) *fakeGitHub {
	return &fakeGitHub{issues: issues, comments: map[int64][]GitHubComment{}, nextID: 1000}
}

func (g *fakeGitHub) ListIssues(context.Context, string) ([]GitHubIssue, error) {
	return append([]GitHubIssue(nil), g.issues...), nil
}

func (g *fakeGitHub) CreateIssue(_ context.Context, _ string, title, body string) (GitHubIssue, error) {
	issue := GitHubIssue{ID: g.nextID, Number: int64(len(g.issues) + 1), Title: title, Body: body, HTMLURL: "https://example.test/issues/new", UpdatedAt: time.Now().UTC()}
	g.nextID++
	g.issues = append(g.issues, issue)
	return issue, nil
}

func (g *fakeGitHub) ListIssueComments(_ context.Context, _ string, number int64) ([]GitHubComment, error) {
	return append([]GitHubComment(nil), g.comments[number]...), nil
}

func (g *fakeGitHub) CreateIssueComment(_ context.Context, _ string, number int64, body string) (GitHubComment, error) {
	comment := GitHubComment{ID: g.nextID, Body: body, HTMLURL: "https://example.test/comments/new", UpdatedAt: time.Now().UTC()}
	g.nextID++
	g.comments[number] = append(g.comments[number], comment)
	return comment, nil
}

func (g *fakeGitHub) SetRepositoryVisibility(_ context.Context, _ string, visibility string) error {
	g.visibility = visibility
	return nil
}
