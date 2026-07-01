package guidance

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type GuidanceStore interface {
	Ping(ctx context.Context) error
	UpsertEntry(ctx context.Context, entry *Entry) (*Entry, error)
	ListEntries(ctx context.Context, projectID string, includeGlobal bool) ([]Entry, error)
	DeleteEntry(ctx context.Context, projectID string, entryID int64) (bool, error)
	DocumentReferences(ctx context.Context, documentProjectID string, documentSlug string) ([]DocumentReference, error)
}

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type DocumentReader interface {
	GetDocument(ctx context.Context, projectID string, slug string) (*Document, error)
}

type Service struct {
	store          GuidanceStore
	projects       ProjectValidator
	documents      DocumentReader
	clock          func() time.Time
	maxPacketBytes int
}

func NewService(store GuidanceStore, projects ProjectValidator, documents DocumentReader, clock func() time.Time, maxPacketBytes int) *Service {
	if maxPacketBytes <= 0 {
		maxPacketBytes = defaultMaxPacketBytes
	}
	return &Service{store: store, projects: projects, documents: documents, clock: clock, maxPacketBytes: maxPacketBytes}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) AddEntry(ctx context.Context, projectID string, req AddEntryRequest) (*Entry, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	documentProjectID := strings.TrimSpace(req.DocumentProjectID)
	if documentProjectID == "" {
		documentProjectID = projectID
	}
	document, err := s.documents.GetDocument(ctx, documentProjectID, req.DocumentSlug)
	if err != nil {
		return nil, err
	}
	if document.Visibility != VisibilityNormal {
		return nil, validationFailed(fmt.Errorf("%w: %s/%s is %s", ErrDocumentNotVisible, documentProjectID, req.DocumentSlug, document.Visibility))
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	entry, err := NewEntry(EntryParams{
		ProjectID:         projectID,
		DocumentProjectID: document.ProjectID,
		DocumentSlug:      document.Slug,
		Importance:        req.Importance,
		Audience:          req.Audience,
		SortOrder:         req.SortOrder,
		Notes:             req.Notes,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.UpsertEntry(ctx, entry)
}

func (s *Service) ListEntries(ctx context.Context, projectID string, includeGlobal bool) ([]Entry, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	return s.store.ListEntries(ctx, projectID, includeGlobal)
}

func (s *Service) DeleteEntry(ctx context.Context, projectID string, entryID int64) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return validationFailed(ErrMissingProjectID)
	}
	if entryID <= 0 {
		return validationFailed(fmt.Errorf("%w: entry_id must be positive", ErrEntryNotFound))
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return err
	}
	deleted, err := s.store.DeleteEntry(ctx, projectID, entryID)
	if err != nil {
		return err
	}
	if !deleted {
		return entryNotFound(projectID, entryID)
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, query ResolveQuery) (GuidancePacket, error) {
	projectID := strings.TrimSpace(query.ProjectID)
	if projectID == "" {
		return GuidancePacket{}, validationFailed(ErrMissingProjectID)
	}
	maxBytes := query.MaxBytes
	if maxBytes <= 0 || maxBytes > s.maxPacketBytes {
		maxBytes = s.maxPacketBytes
	}
	entries, err := s.store.ListEntries(ctx, projectID, projectID != GlobalProjectID)
	if err != nil {
		return GuidancePacket{}, err
	}
	packet := GuidancePacket{
		ProjectID:  projectID,
		ResolvedAt: s.clock().UTC(),
	}
	builder := strings.Builder{}
	builder.WriteString("# Den Agent Guidance for ")
	builder.WriteString(projectID)
	builder.WriteString("\n\n")
	for _, entry := range entries {
		if !audienceMatches(entry.Audience, query.Audience) {
			continue
		}
		document, err := s.documents.GetDocument(ctx, entry.DocumentProjectID, entry.DocumentSlug)
		if err != nil {
			packet.SkippedSources = append(packet.SkippedSources, skipped(entry, "missing_or_unavailable_document"))
			packet.Incomplete = true
			continue
		}
		if document.Visibility == VisibilityArchived {
			packet.SkippedSources = append(packet.SkippedSources, skipped(entry, "archived_document"))
			packet.Incomplete = true
			continue
		}
		if document.Visibility == VisibilityHidden && !query.IncludeHidden {
			packet.SkippedSources = append(packet.SkippedSources, skipped(entry, "hidden_document"))
			packet.Incomplete = true
			continue
		}
		section := guidanceSection(entry, document, query.IncludeContent)
		if builder.Len()+len(section) > maxBytes {
			packet.SkippedSources = append(packet.SkippedSources, skipped(entry, "guidance_packet_byte_budget_exceeded"))
			packet.Truncated = true
			packet.Incomplete = true
			continue
		}
		builder.WriteString(section)
		packet.Sources = append(packet.Sources, GuidanceSource{
			EntryID:           entry.ID,
			SourceScope:       entry.ProjectID,
			DocumentProjectID: document.ProjectID,
			DocumentSlug:      document.Slug,
			DocumentTitle:     document.Title,
			DocumentType:      document.DocType,
			DocumentUpdatedAt: document.UpdatedAt,
			Visibility:        document.Visibility,
			Importance:        entry.Importance,
			Audience:          append([]string(nil), entry.Audience...),
			SortOrder:         entry.SortOrder,
			Notes:             entry.Notes,
			ContentBytes:      len(document.Content),
		})
	}
	packet.ContentMarkdown = builder.String()
	packet.ContentBytes = len(packet.ContentMarkdown)
	packet.ContentSHA256 = packetDigest(packet.ContentMarkdown)
	return packet, nil
}

func (s *Service) DocumentReferences(ctx context.Context, documentProjectID string, documentSlug string) ([]DocumentReference, error) {
	documentProjectID = strings.TrimSpace(documentProjectID)
	documentSlug = strings.TrimSpace(documentSlug)
	if documentProjectID == "" {
		return nil, validationFailed(ErrMissingDocumentProject)
	}
	if documentSlug == "" {
		return nil, validationFailed(ErrMissingDocumentSlug)
	}
	return s.store.DocumentReferences(ctx, documentProjectID, documentSlug)
}

func guidanceSection(entry Entry, document *Document, includeContent bool) string {
	builder := strings.Builder{}
	builder.WriteString("## ")
	builder.WriteString(document.Title)
	builder.WriteString("\n\n")
	builder.WriteString("- Source: `")
	builder.WriteString(document.ProjectID)
	builder.WriteString("/")
	builder.WriteString(document.Slug)
	builder.WriteString("`\n")
	builder.WriteString("- Importance: ")
	builder.WriteString(entry.Importance)
	builder.WriteString("\n")
	builder.WriteString("- Scope: ")
	builder.WriteString(entry.ProjectID)
	builder.WriteString("\n")
	if len(entry.Audience) > 0 {
		builder.WriteString("- Audience: ")
		builder.WriteString(strings.Join(entry.Audience, ", "))
		builder.WriteString("\n")
	}
	if entry.Notes != "" {
		builder.WriteString("- Notes: ")
		builder.WriteString(entry.Notes)
		builder.WriteString("\n")
	}
	if includeContent {
		builder.WriteString("\n")
		builder.WriteString(document.Content)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	return builder.String()
}

func skipped(entry Entry, reason string) SkippedSource {
	return SkippedSource{
		EntryID:           entry.ID,
		SourceScope:       entry.ProjectID,
		DocumentProjectID: entry.DocumentProjectID,
		DocumentSlug:      entry.DocumentSlug,
		Importance:        entry.Importance,
		Reason:            reason,
		Required:          entry.Importance == ImportanceRequired,
	}
}

func audienceMatches(entryAudience []string, requested []string) bool {
	if len(requested) == 0 || len(entryAudience) == 0 {
		return true
	}
	wanted := make(map[string]struct{}, len(requested))
	for _, value := range requested {
		wanted[strings.TrimSpace(value)] = struct{}{}
	}
	for _, value := range entryAudience {
		if _, ok := wanted[value]; ok {
			return true
		}
	}
	return false
}
