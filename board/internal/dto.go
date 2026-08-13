package board

import (
	"encoding/json"
	"time"
)

type PostResponse struct {
	ID             int64           `json:"id"`
	ProjectID      string          `json:"project_id"`
	Title          string          `json:"title"`
	BodyMarkdown   string          `json:"body_markdown"`
	AuthorIdentity string          `json:"author_identity"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type PostSummaryResponse struct {
	ID             int64     `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	AuthorIdentity string    `json:"author_identity"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CommentResponse struct {
	ID              int64           `json:"id"`
	PostID          int64           `json:"post_id"`
	ParentCommentID *int64          `json:"parent_comment_id,omitempty"`
	AuthorIdentity  string          `json:"author_identity,omitempty"`
	BodyMarkdown    string          `json:"body_markdown,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	Status          string          `json:"status"`
	Deleted         bool            `json:"deleted,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type SearchResultResponse struct {
	Kind           string    `json:"kind"`
	ID             int64     `json:"id"`
	PostID         int64     `json:"post_id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title,omitempty"`
	AuthorIdentity string    `json:"author_identity,omitempty"`
	Snippet        string    `json:"snippet"`
	Rank           float64   `json:"rank"`
	CreatedAt      time.Time `json:"created_at"`
}

type PostPageResponse struct {
	Posts       []PostSummaryResponse `json:"posts"`
	NextAfterID *int64                `json:"next_after_id,omitempty"`
}

type CommentPageResponse struct {
	Comments    []CommentResponse `json:"comments"`
	NextAfterID *int64            `json:"next_after_id,omitempty"`
}

type SearchPageResponse struct {
	Results     []SearchResultResponse `json:"results"`
	NextAfterID *int64                 `json:"next_after_id,omitempty"`
}

type CommentPathResponse struct {
	Post      PostResponse      `json:"post"`
	Comments  []CommentResponse `json:"comments"`
	Truncated bool              `json:"truncated"`
}

func toPostResponse(post *Post) PostResponse {
	return PostResponse{
		ID:             post.ID,
		ProjectID:      post.ProjectID,
		Title:          post.Title,
		BodyMarkdown:   post.BodyMarkdown,
		AuthorIdentity: post.AuthorIdentity,
		Metadata:       json.RawMessage(cloneBytes(post.MetadataJSON)),
		Status:         post.Status,
		CreatedAt:      post.CreatedAt,
		UpdatedAt:      post.UpdatedAt,
	}
}

func toPostSummaryResponse(post PostSummary) PostSummaryResponse {
	return PostSummaryResponse{
		ID:             post.ID,
		ProjectID:      post.ProjectID,
		Title:          post.Title,
		AuthorIdentity: post.AuthorIdentity,
		Status:         PostStatusActive,
		CreatedAt:      post.CreatedAt,
		UpdatedAt:      post.UpdatedAt,
	}
}

func toCommentResponse(comment Comment) CommentResponse {
	response := CommentResponse{
		ID:              comment.ID,
		PostID:          comment.PostID,
		ParentCommentID: cloneInt64Pointer(comment.ParentCommentID),
		Status:          comment.Status,
		CreatedAt:       comment.CreatedAt,
		UpdatedAt:       comment.UpdatedAt,
	}
	if comment.Status == CommentStatusDeleted {
		response.Deleted = true
		return response
	}
	response.AuthorIdentity = comment.AuthorIdentity
	response.BodyMarkdown = comment.BodyMarkdown
	response.Metadata = json.RawMessage(cloneBytes(comment.MetadataJSON))
	return response
}

func toPostPageResponse(page PostPage) PostPageResponse {
	posts := make([]PostSummaryResponse, 0, len(page.Posts))
	for _, post := range page.Posts {
		posts = append(posts, toPostSummaryResponse(post))
	}
	return PostPageResponse{Posts: posts, NextAfterID: page.NextAfterID}
}

func toCommentPageResponse(page CommentPage) CommentPageResponse {
	comments := make([]CommentResponse, 0, len(page.Comments))
	for _, comment := range page.Comments {
		comments = append(comments, toCommentResponse(comment))
	}
	return CommentPageResponse{Comments: comments, NextAfterID: page.NextAfterID}
}

func toSearchPageResponse(page SearchPage) SearchPageResponse {
	results := make([]SearchResultResponse, 0, len(page.Results))
	for _, result := range page.Results {
		results = append(results, SearchResultResponse{
			Kind:           result.Kind,
			ID:             result.ID,
			PostID:         result.PostID,
			ProjectID:      result.ProjectID,
			Title:          result.Title,
			AuthorIdentity: result.AuthorIdentity,
			Snippet:        result.Snippet,
			Rank:           result.Rank,
			CreatedAt:      result.CreatedAt,
		})
	}
	return SearchPageResponse{Results: results, NextAfterID: page.NextAfterID}
}

func toCommentPathResponse(path CommentPath) CommentPathResponse {
	comments := make([]CommentResponse, 0, len(path.Comments))
	for _, comment := range path.Comments {
		comments = append(comments, toCommentResponse(comment))
	}
	return CommentPathResponse{
		Post:      toPostResponse(path.Post),
		Comments:  comments,
		Truncated: path.Truncated,
	}
}
