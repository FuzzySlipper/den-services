package docpublish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type PublicationStore interface {
	Get(ctx context.Context, id string) (*PublicationRecord, error)
	FindBySource(ctx context.Context, source DocumentSource) (*PublicationRecord, error)
	Save(ctx context.Context, record PublicationRecord) error
}

type DocumentFetcher interface {
	Fetch(ctx context.Context, source DocumentSource) (*SourceDocument, error)
}

type BlogPublisher interface {
	Publish(ctx context.Context, post RenderedPost, overwrite bool, dryRun bool) (string, error)
	Exists(ctx context.Context, postPath string) (bool, error)
}

type Service struct {
	cfg       BlogConfig
	store     PublicationStore
	fetcher   DocumentFetcher
	publisher BlogPublisher
	clock     func() time.Time
}

func NewService(cfg BlogConfig, store PublicationStore, fetcher DocumentFetcher, publisher BlogPublisher, clock func() time.Time) (*Service, error) {
	if store == nil {
		return nil, invalidRequest("publication store is required")
	}
	if fetcher == nil {
		return nil, invalidRequest("document fetcher is required")
	}
	if publisher == nil {
		return nil, invalidRequest("blog publisher is required")
	}
	if clock == nil {
		return nil, invalidRequest("clock is required")
	}
	return &Service{cfg: cfg, store: store, fetcher: fetcher, publisher: publisher, clock: clock}, nil
}

func (s *Service) Preview(ctx context.Context, req PublicationRequest) (*PublicationResponse, error) {
	if err := req.Validate(true); err != nil {
		return nil, err
	}
	document, warnings, err := s.documentForRequest(ctx, req, false)
	if err != nil {
		return nil, err
	}
	response, _, err := s.prepare(ctx, req, document, PublicationStatusPreviewed, true)
	if err != nil {
		return nil, err
	}
	response.Warnings = warnings
	return response, nil
}

func (s *Service) Publish(ctx context.Context, req PublicationRequest) (*PublicationResponse, error) {
	if err := req.Validate(true); err != nil {
		return nil, err
	}
	document, warnings, err := s.documentForRequest(ctx, req, true)
	if err != nil {
		return nil, err
	}
	response, record, err := s.prepare(ctx, req, document, PublicationStatusPublished, false)
	if err != nil {
		return nil, err
	}
	effectiveOverwrite := req.Options.Overwrite
	if previous, err := s.store.FindBySource(ctx, req.Source); err == nil && previous != nil {
		effectiveOverwrite = true
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	commit, err := s.publisher.Publish(ctx, RenderedPost{Slug: response.Slug, Path: response.PostPath, Markdown: response.PreviewMarkdown}, effectiveOverwrite, false)
	if err != nil {
		record.Status = PublicationStatusFailed
		record.LastError = err.Error()
		record.UpdatedAt = s.clock().UTC()
		_ = s.store.Save(ctx, *record)
		return nil, err
	}
	record.GitCommit = commit
	record.Status = PublicationStatusPublished
	record.UpdatedAt = s.clock().UTC()
	if err := s.store.Save(ctx, *record); err != nil {
		return nil, err
	}
	response.GitCommit = commit
	response.Warnings = warnings
	return response, nil
}

func (s *Service) Get(ctx context.Context, id string) (*PublicationRecord, error) {
	if id == "" {
		return nil, invalidRequest("publication id is required")
	}
	return s.store.Get(ctx, id)
}

func (s *Service) documentForRequest(ctx context.Context, req PublicationRequest, requireFetch bool) (*SourceDocument, []string, error) {
	if requireFetch || req.Document == nil {
		document, err := s.fetcher.Fetch(ctx, req.Source)
		if err != nil {
			return nil, nil, err
		}
		return document, nil, nil
	}
	return req.Document, []string{"using request document payload for preview only; publish fetches canonical source"}, nil
}

func (s *Service) prepare(ctx context.Context, req PublicationRequest, document *SourceDocument, status PublicationStatus, dryRun bool) (*PublicationResponse, *PublicationRecord, error) {
	rendered, err := RenderPost(document, req.Options, s.cfg.PostDir, s.cfg.PublicBaseURL, s.clock())
	if err != nil {
		return nil, nil, err
	}
	previous, err := s.store.FindBySource(ctx, req.Source)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, nil, err
	}
	if previous == nil {
		exists, err := s.publisher.Exists(ctx, rendered.Path)
		if err != nil {
			return nil, nil, err
		}
		if exists && !req.Options.Overwrite && !dryRun {
			return nil, nil, invalidRequest("post path already exists and overwrite is false")
		}
	}
	id := ""
	createdAt := s.clock().UTC()
	if previous != nil {
		id = previous.ID
		createdAt = previous.CreatedAt
	}
	if id == "" {
		var err error
		id, err = newPublicationID()
		if err != nil {
			return nil, nil, err
		}
	}
	record := &PublicationRecord{
		ID:                id,
		SourceProjectID:   req.Source.ProjectID,
		DocumentProjectID: req.Source.DocumentProjectID,
		DocumentSlug:      req.Source.DocumentSlug,
		SourceVersion:     document.UpdatedAt.Format(time.RFC3339),
		Title:             rendered.Title,
		Slug:              rendered.Slug,
		RepoID:            filepath.Base(s.cfg.RepoPath),
		Branch:            s.cfg.Branch,
		PostPath:          rendered.Path,
		PublicURL:         rendered.PublicURL,
		Status:            status,
		RequestedBy:       req.RequestedBy,
		CreatedAt:         createdAt,
		UpdatedAt:         s.clock().UTC(),
	}
	response := &PublicationResponse{
		PublicationID:   record.ID,
		Status:          status,
		DryRun:          dryRun,
		Title:           rendered.Title,
		Slug:            rendered.Slug,
		PostPath:        rendered.Path,
		PublicURL:       rendered.PublicURL,
		PreviewMarkdown: rendered.Markdown,
		Source:          req.Source,
	}
	if dryRun {
		return response, record, nil
	}
	return response, record, nil
}

type RenderedPost struct {
	Title     string
	Slug      string
	Path      string
	PublicURL string
	Markdown  string
}

func RenderPost(document *SourceDocument, options PublicationOptions, postDir string, publicBaseURL string, now time.Time) (RenderedPost, error) {
	if document == nil {
		return RenderedPost{}, invalidRequest("document is required")
	}
	title := strings.TrimSpace(firstNonEmpty(options.Title, document.Title))
	if title == "" {
		return RenderedPost{}, invalidRequest("document title is required")
	}
	slug := strings.TrimSpace(firstNonEmpty(options.Slug, document.Slug, title))
	slug = Slugify(slug)
	if slug == "" {
		return RenderedPost{}, invalidRequest("slug is required")
	}
	body := stripFrontmatter(document.Markdown)
	date := now.UTC().Format("2006-01-02")
	postPath := filepath.ToSlash(filepath.Join(postDir, date+"-"+slug+".md"))
	publicURL := strings.TrimRight(publicBaseURL, "/") + "/" + slug + "/"
	markdown := renderFrontmatter(title, now.UTC(), options.Tags) + "\n" + strings.TrimLeft(body, "\n")
	return RenderedPost{Title: title, Slug: slug, Path: postPath, PublicURL: publicURL, Markdown: markdown}, nil
}

func Slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	nonSlug := regexp.MustCompile(`[^a-z0-9\s-]+`)
	value = nonSlug.ReplaceAllString(value, "")
	spaces := regexp.MustCompile(`[\s-]+`)
	value = spaces.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 80 {
		value = strings.Trim(value[:80], "-")
	}
	return value
}

func stripFrontmatter(markdown string) string {
	if !strings.HasPrefix(markdown, "---\n") {
		return markdown
	}
	rest := markdown[4:]
	if index := strings.Index(rest, "\n---\n"); index >= 0 {
		return rest[index+5:]
	}
	return markdown
}

func renderFrontmatter(title string, date time.Time, tags []string) string {
	lines := []string{
		"---",
		"layout: post",
		"title: " + quoteYAML(title),
		"date: " + quoteYAML(date.Format("2006-01-02 15:04:05 -0700")),
	}
	if len(tags) > 0 {
		quoted := make([]string, 0, len(tags))
		for _, tag := range tags {
			if slug := Slugify(tag); slug != "" {
				quoted = append(quoted, quoteYAML(slug))
			}
		}
		if len(quoted) > 0 {
			lines = append(lines, "tags: ["+strings.Join(quoted, ", ")+"]")
		}
	}
	lines = append(lines, "---", "")
	return strings.Join(lines, "\n")
}

func quoteYAML(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func newPublicationID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generating publication id: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
