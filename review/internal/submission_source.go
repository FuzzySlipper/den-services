package review

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (s *Service) validateReviewSource(ctx context.Context, source ReviewSourceRequest) error {
	if !validGitHubRepository(source.Repository) || !validGitHubSHA(source.CommitSHA) || !validGitHubSHA(source.BaseCommit) {
		return validationError(errors.New("source requires repository owner/name and full commit_sha and base_commit SHAs"), "invalid_review_source", "source", "review_request")
	}
	validator, ok := s.githubChecks.(ReviewSourceValidator)
	if !ok {
		return NewServiceError(errors.New("GitHub source validation is unavailable; retry after restoring the Review GitHub configuration"), "source_validation_unavailable", http.StatusServiceUnavailable)
	}
	return validator.ValidateReviewSource(ctx, source)
}

// ValidateReviewSource checks both objects and their ancestry in one compare
// request. A bad range otherwise creates an impossible gate or the wrong diff.
func (c *GitHubClient) ValidateReviewSource(ctx context.Context, source ReviewSourceRequest) error {
	var comparison githubCompareResponse
	path := "/repos/" + source.Repository + "/compare/" + url.PathEscape(source.BaseCommit+"..."+source.CommitSHA)
	if err := c.getJSON(ctx, path, &comparison); err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && (githubErr.StatusCode == http.StatusNotFound || githubErr.StatusCode == http.StatusUnprocessableEntity) {
			if strings.Contains(strings.ToLower(githubErr.Message), "no common ancestor") {
				return validationError(err, "base_not_ancestor", "base_commit", "review_request")
			}
			return validationError(fmt.Errorf("commit_sha or base_commit does not exist in %s: %w", source.Repository, err), "unknown_commit", "source", "review_request")
		}
		return NewServiceError(fmt.Errorf("checking review source: %w", err), "source_validation_unavailable", http.StatusServiceUnavailable)
	}
	if comparison.Status != "ahead" && comparison.Status != "identical" {
		return validationError(fmt.Errorf("base %s is not an ancestor of commit %s in %s (%s)", source.BaseCommit, source.CommitSHA, source.Repository, comparison.Status), "base_not_ancestor", "base_commit", "review_request")
	}
	return nil
}

func supersessionError(message string) error {
	return conflict(errors.New(message), "round_not_supersedable")
}

func supersessionNotes(roundID int64, reason string) string {
	return fmt.Sprintf("Superseded by review round %d: %s", roundID, strings.TrimSpace(reason))
}
