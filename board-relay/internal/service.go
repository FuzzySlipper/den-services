package boardrelay

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const pageSize = 100

type Service struct {
	store      MappingStore
	board      BoardClient
	github     GitHubClient
	repository string
	clock      func() time.Time
}

func NewService(store MappingStore, board BoardClient, github GitHubClient, repository string, clock func() time.Time) (*Service, error) {
	repository, err := normalizeRepository(repository)
	if err != nil {
		return nil, err
	}
	if store == nil || board == nil || github == nil || clock == nil {
		return nil, fmt.Errorf("board relay dependencies are required")
	}
	return &Service{store: store, board: board, github: github, repository: repository, clock: clock}, nil
}

func (s *Service) CheckStore(ctx context.Context) error { return s.store.Ping(ctx) }

func (s *Service) Sync(ctx context.Context, rawProjectID string) (SyncReceipt, error) {
	projectID, err := normalizeProjectID(rawProjectID)
	if err != nil {
		return SyncReceipt{}, validationFailed(err)
	}
	receipt := SyncReceipt{ProjectID: projectID, Repository: s.repository, ItemURLs: make([]string, 0)}
	issues, err := s.github.ListIssues(ctx, s.repository)
	if err != nil {
		return SyncReceipt{}, err
	}
	if err := s.importIssues(ctx, projectID, issues, &receipt); err != nil {
		return SyncReceipt{}, err
	}
	if err := s.exportPosts(ctx, projectID, issues, &receipt); err != nil {
		return SyncReceipt{}, err
	}
	if err := s.importIssueComments(ctx, projectID, issues, &receipt); err != nil {
		return SyncReceipt{}, err
	}
	sort.Strings(receipt.ItemURLs)
	return receipt, nil
}

func (s *Service) SetVisibility(ctx context.Context, request VisibilityRequest) error {
	visibility, err := normalizeVisibility(request.Visibility)
	if err != nil {
		return validationFailed(err)
	}
	return s.github.SetRepositoryVisibility(ctx, s.repository, visibility)
}

func (s *Service) importIssues(ctx context.Context, projectID string, issues []GitHubIssue, receipt *SyncReceipt) error {
	for _, issue := range issues {
		marker, marked := parseMarker(issue.Body)
		if !marked || marker.ProjectID != projectID || marker.BoardPostID > 0 {
			continue
		}
		mapping, err := s.store.FindByGitHub(ctx, projectID, "issue", issue.ID)
		if err != nil {
			return err
		}
		if mapping != nil {
			if issue.UpdatedAt.After(mapping.GitHubUpdatedAt) {
				receipt.UnsupportedRemoteEdits++
			}
			continue
		}
		post, err := s.board.CreatePost(ctx, projectID, BoardCreatePostRequest{
			Title: issue.Title, BodyMarkdown: issue.Body, AuthorIdentity: githubIdentity(issue.Login),
			Metadata:       relayMetadata("issue", issue.ID, issue.HTMLURL, issue.Login, issue.CreatedAt),
			IdempotencyKey: githubPostKey(projectID, issue.ID),
		})
		if err != nil {
			return fmt.Errorf("importing github issue %d: %w", issue.ID, err)
		}
		if err := s.store.Save(ctx, newMapping(projectID, itemKindPost, post.ID, "issue", issue.ID, issue.Number, issue.HTMLURL, "github", issue.UpdatedAt, s.clock())); err != nil {
			return err
		}
		receipt.ImportedPosts++
		receipt.ItemURLs = append(receipt.ItemURLs, issue.HTMLURL)
	}
	return nil
}

func (s *Service) exportPosts(ctx context.Context, projectID string, issues []GitHubIssue, receipt *SyncReceipt) error {
	issueMarkers := make(map[int64]GitHubIssue)
	for _, issue := range issues {
		marker, marked := parseMarker(issue.Body)
		if marked && marker.ProjectID == projectID && marker.BoardPostID > 0 {
			issueMarkers[marker.BoardPostID] = issue
		}
	}
	var afterID *int64
	for {
		page, err := s.board.ListPosts(ctx, projectID, afterID, pageSize)
		if err != nil {
			return fmt.Errorf("listing board posts for export: %w", err)
		}
		for _, post := range page.Posts {
			if post.Status != "active" {
				continue
			}
			fullPost, err := s.board.GetPost(ctx, post.ID)
			if err != nil {
				return fmt.Errorf("reading board post %d for export: %w", post.ID, err)
			}
			if fullPost == nil || fullPost.Status != "active" {
				receipt.SkippedItems++
				continue
			}
			post = *fullPost
			mapping, err := s.store.FindByBoard(ctx, projectID, itemKindPost, post.ID)
			if err != nil {
				return err
			}
			if mapping == nil {
				if recovered, found := issueMarkers[post.ID]; found {
					if err := s.store.Save(ctx, newMapping(projectID, itemKindPost, post.ID, "issue", recovered.ID, recovered.Number, recovered.HTMLURL, "board", recovered.UpdatedAt, s.clock())); err != nil {
						return err
					}
					receipt.RecoveredMappings++
					mapping, err = s.store.FindByBoard(ctx, projectID, itemKindPost, post.ID)
					if err != nil {
						return err
					}
				}
			}
			if mapping == nil {
				issue, err := s.github.CreateIssue(ctx, s.repository, post.Title, post.BodyMarkdown+"\n\n"+postMarker(projectID, post.ID))
				if err != nil {
					return fmt.Errorf("exporting board post %d: %w", post.ID, err)
				}
				if err := s.store.Save(ctx, newMapping(projectID, itemKindPost, post.ID, "issue", issue.ID, issue.Number, issue.HTMLURL, "board", issue.UpdatedAt, s.clock())); err != nil {
					return err
				}
				receipt.ExportedPosts++
				receipt.ItemURLs = append(receipt.ItemURLs, issue.HTMLURL)
				mapping, err = s.store.FindByBoard(ctx, projectID, itemKindPost, post.ID)
				if err != nil {
					return err
				}
			}
			if mapping != nil {
				if err := s.exportComments(ctx, projectID, post.ID, mapping.IssueNumber, nil, receipt); err != nil {
					return err
				}
			}
		}
		if page.NextAfterID == nil {
			return nil
		}
		afterID = page.NextAfterID
	}
}

func (s *Service) exportComments(ctx context.Context, projectID string, postID int64, issueNumber int64, parentCommentID *int64, receipt *SyncReceipt) error {
	var afterID *int64
	for {
		page, err := s.board.ListComments(ctx, postID, parentCommentID, afterID, pageSize)
		if err != nil {
			return fmt.Errorf("listing board comments for export: %w", err)
		}
		for _, comment := range page.Comments {
			if comment.Status != "active" {
				continue
			}
			mapping, err := s.store.FindByBoard(ctx, projectID, itemKindComment, comment.ID)
			if err != nil {
				return err
			}
			if mapping == nil {
				body := comment.BodyMarkdown + "\n\n" + commentMarker(projectID, comment.ID, comment.ParentCommentID)
				remote, err := s.github.CreateIssueComment(ctx, s.repository, issueNumber, body)
				if err != nil {
					return fmt.Errorf("exporting board comment %d: %w", comment.ID, err)
				}
				if err := s.store.Save(ctx, newMapping(projectID, itemKindComment, comment.ID, "comment", remote.ID, issueNumber, remote.HTMLURL, "board", remote.UpdatedAt, s.clock())); err != nil {
					return err
				}
				receipt.ExportedComments++
				receipt.ItemURLs = append(receipt.ItemURLs, remote.HTMLURL)
			}
			commentID := comment.ID
			if err := s.exportComments(ctx, projectID, postID, issueNumber, &commentID, receipt); err != nil {
				return err
			}
		}
		if page.NextAfterID == nil {
			return nil
		}
		afterID = page.NextAfterID
	}
}

func (s *Service) importIssueComments(ctx context.Context, projectID string, issues []GitHubIssue, receipt *SyncReceipt) error {
	for _, issue := range issues {
		postMapping, err := s.store.FindByGitHub(ctx, projectID, "issue", issue.ID)
		if err != nil || postMapping == nil {
			if err != nil {
				return err
			}
			continue
		}
		comments, err := s.github.ListIssueComments(ctx, s.repository, issue.Number)
		if err != nil {
			return fmt.Errorf("listing github comments for issue %d: %w", issue.Number, err)
		}
		for _, remote := range comments {
			marker, marked := parseMarker(remote.Body)
			if marked && marker.ProjectID == projectID && marker.BoardCommentID > 0 {
				if err := s.recoverCommentMapping(ctx, projectID, postMapping.BoardID, issue.Number, remote, marker, receipt); err != nil {
					return err
				}
				continue
			}
			mapping, err := s.store.FindByGitHub(ctx, projectID, "comment", remote.ID)
			if err != nil {
				return err
			}
			if mapping != nil {
				if remote.UpdatedAt.After(mapping.GitHubUpdatedAt) {
					receipt.UnsupportedRemoteEdits++
				}
				continue
			}
			parentID := parentFromMarker(projectID, postMapping.BoardID, marked, marker)
			comment, err := s.board.CreateComment(ctx, postMapping.BoardID, BoardCreateCommentRequest{
				ParentCommentID: parentID, BodyMarkdown: remote.Body, AuthorIdentity: githubIdentity(remote.Login),
				Metadata:       relayMetadata("comment", remote.ID, remote.HTMLURL, remote.Login, remote.CreatedAt),
				IdempotencyKey: githubCommentKey(projectID, remote.ID),
			})
			if err != nil {
				return fmt.Errorf("importing github comment %d: %w", remote.ID, err)
			}
			if err := s.store.Save(ctx, newMapping(projectID, itemKindComment, comment.ID, "comment", remote.ID, issue.Number, remote.HTMLURL, "github", remote.UpdatedAt, s.clock())); err != nil {
				return err
			}
			receipt.ImportedComments++
			receipt.ItemURLs = append(receipt.ItemURLs, remote.HTMLURL)
		}
	}
	return nil
}

func (s *Service) recoverCommentMapping(ctx context.Context, projectID string, postID int64, issueNumber int64, remote GitHubComment, marker relayMarker, receipt *SyncReceipt) error {
	mapping, err := s.store.FindByGitHub(ctx, projectID, "comment", remote.ID)
	if err != nil || mapping != nil {
		return err
	}
	post, err := s.board.GetPost(ctx, postID)
	if err != nil {
		return err
	}
	if post == nil {
		return ErrBoardPostNotFound
	}
	comment, err := s.board.GetComment(ctx, marker.BoardCommentID)
	if err != nil {
		return err
	}
	if comment == nil || comment.PostID != postID {
		return ErrBoardCommentAbsent
	}
	if err := s.store.Save(ctx, newMapping(projectID, itemKindComment, marker.BoardCommentID, "comment", remote.ID, issueNumber, remote.HTMLURL, "board", remote.UpdatedAt, s.clock())); err != nil {
		return err
	}
	receipt.RecoveredMappings++
	return nil
}

func parentFromMarker(projectID string, postID int64, marked bool, marker relayMarker) *int64 {
	if !marked || marker.ProjectID != projectID || marker.ParentCommentID <= 0 {
		return nil
	}
	parentID := marker.ParentCommentID
	return &parentID
}

func githubIdentity(login string) string {
	login = strings.TrimSpace(login)
	if login == "" {
		return "github:unknown"
	}
	return "github:" + login
}

func githubPostKey(projectID string, issueID int64) string {
	return fmt.Sprintf("github-relay:%s:issue:%d", projectID, issueID)
}

func githubCommentKey(projectID string, commentID int64) string {
	return fmt.Sprintf("github-relay:%s:comment:%d", projectID, commentID)
}
