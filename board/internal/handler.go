package board

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

type BoardUseCases interface {
	CreatePost(ctx context.Context, projectID string, req CreatePostRequest) (*Post, error)
	ListPosts(ctx context.Context, projectID string, query ListPostsQuery) (PostPage, error)
	Search(ctx context.Context, projectID string, query SearchQuery) (SearchPage, error)
	GetPost(ctx context.Context, id int64) (*Post, error)
	PurgePost(ctx context.Context, id int64, req PurgeRequest) error
	CreateComment(ctx context.Context, postID int64, req CreateCommentRequest) (*Comment, error)
	ListComments(ctx context.Context, postID int64, query ListCommentsQuery) (CommentPage, error)
	GetComment(ctx context.Context, id int64) (*Comment, error)
	GetCommentPath(ctx context.Context, id int64, limit int) (CommentPath, error)
	PurgeComment(ctx context.Context, id int64, req PurgeRequest) error
}

type Handler struct {
	service BoardUseCases
}

func NewHandler(service BoardUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/board/posts", h.createPost)
	mux.HandleFunc("GET /v1/projects/{project_id}/board/posts", h.listPosts)
	mux.HandleFunc("GET /v1/projects/{project_id}/board/posts/search", h.searchPosts)
	mux.HandleFunc("GET /v1/board/posts/{post_id}", h.getPost)
	mux.HandleFunc("DELETE /v1/board/posts/{post_id}", h.purgePost)
	mux.HandleFunc("POST /v1/board/posts/{post_id}/comments", h.createComment)
	mux.HandleFunc("GET /v1/board/posts/{post_id}/comments", h.listComments)
	mux.HandleFunc("GET /v1/board/comments/{comment_id}", h.getComment)
	mux.HandleFunc("GET /v1/board/comments/{comment_id}/path", h.getCommentPath)
	mux.HandleFunc("DELETE /v1/board/comments/{comment_id}", h.purgeComment)
}

func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	var req CreatePostRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	post, err := h.service.CreatePost(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toPostResponse(post))
}

func (h *Handler) listPosts(w http.ResponseWriter, r *http.Request) {
	afterID, limit, err := pageQuery(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	page, err := h.service.ListPosts(r.Context(), r.PathValue("project_id"), ListPostsQuery{
		AfterID: afterID,
		Limit:   limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toPostPageResponse(page))
}

func (h *Handler) searchPosts(w http.ResponseWriter, r *http.Request) {
	afterID, limit, err := pageQuery(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	page, err := h.service.Search(r.Context(), r.PathValue("project_id"), SearchQuery{
		Query:   r.URL.Query().Get("q"),
		AfterID: afterID,
		Limit:   limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toSearchPageResponse(page))
}

func (h *Handler) getPost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "post_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	post, err := h.service.GetPost(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toPostResponse(post))
}

func (h *Handler) purgePost(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "post_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req PurgeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.PurgePost(r.Context(), id, req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createComment(w http.ResponseWriter, r *http.Request) {
	postID, err := pathID(r, "post_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req CreateCommentRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	comment, err := h.service.CreateComment(r.Context(), postID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toCommentResponse(*comment))
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
	postID, err := pathID(r, "post_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	afterID, limit, err := pageQuery(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	parentID, err := optionalParentID(r.URL.Query().Get("parent_comment_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	page, err := h.service.ListComments(r.Context(), postID, ListCommentsQuery{
		ParentCommentID: parentID,
		AfterID:         afterID,
		Limit:           limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toCommentPageResponse(page))
}

func (h *Handler) getComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "comment_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	comment, err := h.service.GetComment(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toCommentResponse(*comment))
}

func (h *Handler) getCommentPath(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "comment_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	limit, err := optionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	path, err := h.service.GetCommentPath(r.Context(), id, limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toCommentPathResponse(path))
}

func (h *Handler) purgeComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "comment_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req PurgeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.PurgeComment(r.Context(), id, req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.PathValue(name))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, badRequest(err)
	}
	if err := validateID(id); err != nil {
		return 0, badRequest(err)
	}
	return id, nil
}

func pageQuery(r *http.Request) (int64, int, error) {
	afterID, err := optionalInt64(r.URL.Query().Get("after_id"))
	if err != nil {
		return 0, 0, err
	}
	limit, err := optionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		return 0, 0, err
	}
	return afterID, limit, nil
}

func optionalInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, badRequest(err)
	}
	return value, nil
}

func optionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	return value, nil
}

func optionalParentID(raw string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := optionalInt64(raw)
	if err != nil {
		return nil, err
	}
	if err := validateID(value); err != nil {
		return nil, badRequest(err)
	}
	return &value, nil
}

var _ = errors.Is
