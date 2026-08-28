package knowledge

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// NavigationStore is kept separate from the entry CRUD seam because maps and
// links are curated navigation metadata, not entry content.
type NavigationStore interface {
	ReplaceLinks(ctx context.Context, fromSlug string, links []EntryLink) error
	ListLinks(ctx context.Context, slug string, limit int) ([]ResolvedLink, error)
	CurrentRevision(ctx context.Context, slug string) (int, error)
	StoreMap(ctx context.Context, knowledgeMap KnowledgeMap) (*KnowledgeMap, error)
	GetMap(ctx context.Context, slug string) (*KnowledgeMap, error)
}

func (s *Service) EntryCard(ctx context.Context, slug string, includeArchived bool) (EntryCardResponse, error) {
	entry, err := s.GetEntry(ctx, slug, includeArchived)
	if err != nil {
		return EntryCardResponse{}, err
	}
	revision, err := s.navigation.CurrentRevision(ctx, entry.Slug())
	if err != nil {
		return EntryCardResponse{}, err
	}
	card := cardFromEntry(entry, revision)
	return s.populateReplacementTarget(ctx, entry, card)
}

func (s *Service) BatchCards(ctx context.Context, slugs []string, includeArchived bool, offset int) (CardsResponse, error) {
	if len(slugs) > MaxBatchCardHandles {
		return CardsResponse{}, validationFailed(fmt.Errorf("knowledge card batch exceeds maximum of %d handles", MaxBatchCardHandles))
	}
	offset = max(offset, 0)
	unique := make([]string, 0, min(len(slugs), MaxBatchCards))
	seen := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}
		unique = append(unique, slug)
	}
	if offset >= len(unique) {
		return CardsResponse{Cards: []EntryCardResponse{}, Missing: []string{}}, nil
	}
	end := min(offset+MaxBatchCards, len(unique))
	response := CardsResponse{Cards: make([]EntryCardResponse, 0, end-offset), Missing: []string{}}
	for _, slug := range unique[offset:end] {
		card, err := s.EntryCard(ctx, slug, includeArchived)
		if err != nil {
			if isEntryNotFound(err) {
				response.Missing = append(response.Missing, slug)
				continue
			}
			return CardsResponse{}, err
		}
		response.Cards = append(response.Cards, card)
	}
	if end < len(unique) {
		next := end
		response.NextOffset = &next
	}
	return response, nil
}

func (s *Service) ReadEntry(ctx context.Context, slug string, view string, sectionID string, knownRevision int, knownDigest string, includeArchived bool) (ReadResponse, error) {
	entry, err := s.GetEntry(ctx, slug, includeArchived)
	if err != nil {
		return ReadResponse{}, err
	}
	revision, err := s.navigation.CurrentRevision(ctx, entry.Slug())
	if err != nil {
		return ReadResponse{}, err
	}
	card, err := s.populateReplacementTarget(ctx, entry, cardFromEntry(entry, revision))
	if err != nil {
		return ReadResponse{}, err
	}
	response := ReadResponse{Slug: entry.Slug(), View: view, Revision: revision, Digest: card.Digest, Card: card, Links: []ResolvedLink{}}
	if knownRevision == revision || (knownDigest != "" && knownDigest == card.Digest) {
		response.Unchanged = true
		return response, nil
	}
	links, err := s.navigation.ListLinks(ctx, entry.Slug(), MaxNavigationLinks)
	if err != nil {
		return ReadResponse{}, err
	}
	response.Links, err = appendReplacementLink(ctx, s, entry, links)
	if err != nil {
		return ReadResponse{}, err
	}
	switch view {
	case "outline":
		response.Outline = outlineMarkdown(entry.BodyMarkdown())
	case "section":
		selected, body, err := selectMarkdownSection(entry.BodyMarkdown(), sectionID)
		if err != nil {
			return ReadResponse{}, err
		}
		response.Section = &selected
		response.Body = body
	case "full":
		response.Body = entry.BodyMarkdown()
	default:
		return ReadResponse{}, validationFailed(fmt.Errorf("invalid knowledge read view: %s", view))
	}
	return response, nil
}

func (s *Service) ReplaceLinks(ctx context.Context, slug string, requests []EntryLinkRequest) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return validationFailed(ErrMissingSlug)
	}
	if _, err := s.GetEntry(ctx, slug, true); err != nil {
		return err
	}
	if len(requests) > MaxNavigationLinks {
		return validationFailed(fmt.Errorf("knowledge links exceed maximum of %d", MaxNavigationLinks))
	}
	links := make([]EntryLink, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		kind := strings.TrimSpace(request.Kind)
		target := strings.TrimSpace(request.ToSlug)
		if !validLinkKind(kind) {
			return validationFailed(fmt.Errorf("%w: %s", ErrInvalidLinkKind, kind))
		}
		if target == "" || target == slug {
			return validationFailed(ErrInvalidLinkTarget)
		}
		if _, err := s.GetEntry(ctx, target, true); err != nil {
			if isEntryNotFound(err) {
				return validationFailed(fmt.Errorf("%w: %s", ErrInvalidLinkTarget, target))
			}
			return err
		}
		key := kind + "\x00" + target
		if _, exists := seen[key]; exists {
			return validationFailed(fmt.Errorf("duplicate knowledge link: %s", target))
		}
		seen[key] = struct{}{}
		links = append(links, EntryLink{FromSlug: slug, ToSlug: target, Kind: kind, Description: strings.TrimSpace(request.Description)})
	}
	return s.navigation.ReplaceLinks(ctx, slug, links)
}

func (s *Service) StoreMap(ctx context.Context, request StoreKnowledgeMapRequest) (*KnowledgeMap, error) {
	mapSlug := strings.TrimSpace(request.Slug)
	if mapSlug == "" || strings.TrimSpace(request.Title) == "" || len(request.Entries) == 0 || len(request.Entries) > MaxMapEntries {
		return nil, validationFailed(ErrInvalidMap)
	}
	entries := make([]KnowledgeMapEntry, 0, len(request.Entries))
	seen := make(map[string]struct{}, len(request.Entries))
	positions := make(map[int]struct{}, len(request.Entries))
	for _, requestEntry := range request.Entries {
		slug := strings.TrimSpace(requestEntry.EntrySlug)
		if slug == "" || requestEntry.Position < 0 || requestEntry.Position >= MaxMapEntries {
			return nil, validationFailed(ErrInvalidMap)
		}
		if _, exists := seen[slug]; exists {
			return nil, validationFailed(fmt.Errorf("knowledge map repeats entry: %s", slug))
		}
		seen[slug] = struct{}{}
		if _, exists := positions[requestEntry.Position]; exists {
			return nil, validationFailed(fmt.Errorf("knowledge map repeats position: %d", requestEntry.Position))
		}
		positions[requestEntry.Position] = struct{}{}
		if _, err := s.GetEntry(ctx, slug, true); err != nil {
			if isEntryNotFound(err) {
				return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidLinkTarget, slug))
			}
			return nil, err
		}
		entries = append(entries, KnowledgeMapEntry{EntrySlug: slug, GroupName: strings.TrimSpace(requestEntry.GroupName), Position: requestEntry.Position, Note: strings.TrimSpace(requestEntry.Note)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Position < entries[j].Position })
	now := s.clock().UTC()
	return s.navigation.StoreMap(ctx, KnowledgeMap{Slug: mapSlug, Title: strings.TrimSpace(request.Title), Summary: strings.TrimSpace(request.Summary), CreatedBy: strings.TrimSpace(request.ChangedBy), UpdatedBy: strings.TrimSpace(request.ChangedBy), CreatedAt: now, UpdatedAt: now, Entries: entries})
}

func (s *Service) GetMap(ctx context.Context, slug string) (KnowledgeMapResponse, error) {
	knowledgeMap, err := s.navigation.GetMap(ctx, strings.TrimSpace(slug))
	if err != nil {
		return KnowledgeMapResponse{}, err
	}
	response := KnowledgeMapResponse{Slug: knowledgeMap.Slug, Title: knowledgeMap.Title, Summary: knowledgeMap.Summary, UpdatedAt: knowledgeMap.UpdatedAt, Entries: make([]KnowledgeMapEntryView, 0, len(knowledgeMap.Entries))}
	for _, entry := range knowledgeMap.Entries {
		card, err := s.EntryCard(ctx, entry.EntrySlug, true)
		if err != nil {
			return KnowledgeMapResponse{}, err
		}
		response.Entries = append(response.Entries, KnowledgeMapEntryView{GroupName: entry.GroupName, Position: entry.Position, Note: entry.Note, Card: card})
	}
	return response, nil
}

func cardFromEntry(entry *Entry, revision int) EntryCardResponse {
	return EntryCardResponse{Slug: entry.Slug(), Title: entry.Title(), Summary: entry.Summary(), Kind: entry.Kind(), Status: entry.Status(), CurationState: entry.CurationState(), Tags: entry.Tags(), SourceRefs: entry.SourceRefs(), ReplacementSlug: entry.ReplacementSlug(), Revision: revision, Digest: entryDigest(entry), LastReviewedAt: entry.LastReviewedAt(), UpdatedAt: entry.UpdatedAt()}
}

func appendReplacementLink(ctx context.Context, service *Service, entry *Entry, links []ResolvedLink) ([]ResolvedLink, error) {
	replacement := entry.ReplacementSlug()
	if replacement == "" || len(links) >= MaxNavigationLinks {
		return links, nil
	}
	for _, link := range links {
		if link.Kind == LinkKindReplacement && link.Target.Slug == replacement {
			return links, nil
		}
	}
	resolved := ResolvedLink{Kind: LinkKindReplacement, Target: LinkTarget{Slug: replacement, Missing: true}}
	if target, err := service.GetEntry(ctx, replacement, true); err == nil {
		resolved.Target = LinkTarget{Slug: target.Slug(), Title: target.Title(), Summary: target.Summary(), Status: target.Status()}
	} else if !isEntryNotFound(err) {
		return nil, err
	}
	return append(links, resolved), nil
}

func (s *Service) populateReplacementTarget(ctx context.Context, entry *Entry, card EntryCardResponse) (EntryCardResponse, error) {
	if entry.ReplacementSlug() == "" {
		return card, nil
	}
	target := LinkTarget{Slug: entry.ReplacementSlug(), Missing: true}
	if replacement, err := s.GetEntry(ctx, entry.ReplacementSlug(), true); err == nil {
		target = LinkTarget{Slug: replacement.Slug(), Title: replacement.Title(), Summary: replacement.Summary(), Status: replacement.Status()}
	} else if !isEntryNotFound(err) {
		return EntryCardResponse{}, err
	}
	card.Replacement = &target
	return card, nil
}

func isEntryNotFound(err error) bool { return errors.Is(err, ErrEntryNotFound) }

var (
	headingPattern        = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*$`)
	closingHeadingPattern = regexp.MustCompile(`[ \t]+#+[ \t]*$`)
) //nolint:gochecknoglobals

type markdownHeading struct {
	OutlineSection
	start int
}

func outlineMarkdown(markdown string) []OutlineSection {
	headings := markdownHeadings(markdown)
	if len(headings) > MaxOutlineSections {
		headings = headings[:MaxOutlineSections]
	}
	sections := make([]OutlineSection, 0, len(headings))
	for _, heading := range headings {
		sections = append(sections, heading.OutlineSection)
	}
	return sections
}

func selectMarkdownSection(markdown string, sectionID string) (OutlineSection, string, error) {
	sectionID = strings.TrimSpace(sectionID)
	if sectionID == "" {
		return OutlineSection{}, "", sectionNotFound(sectionID)
	}
	headings := markdownHeadings(markdown)
	for index, heading := range headings {
		if heading.ID != sectionID {
			continue
		}
		end := len(markdown)
		for _, next := range headings[index+1:] {
			if next.Level <= heading.Level {
				end = next.start
				break
			}
		}
		body := strings.TrimSpace(markdown[heading.start:end])
		if len(body) > MaxSectionBytes {
			return OutlineSection{}, "", validationFailed(fmt.Errorf("%w: %s", ErrSectionTooLarge, sectionID))
		}
		return heading.OutlineSection, body, nil
	}
	return OutlineSection{}, "", sectionNotFound(sectionID)
}

func markdownHeadings(markdown string) []markdownHeading {
	lines := strings.SplitAfter(markdown, "\n")
	headings := []markdownHeading{}
	counts := map[string]int{}
	offset := 0
	var fenceMarker byte
	fenceLength := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		marker, length, isFence := markdownFence(trimmed)
		if fenceMarker == 0 {
			if isFence {
				fenceMarker = marker
				fenceLength = length
			}
		} else if isFence && marker == fenceMarker && length >= fenceLength && markdownFenceCloses(trimmed, length) {
			fenceMarker = 0
			fenceLength = 0
		}
		if fenceMarker == 0 {
			match := headingPattern.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
			if len(match) == 3 {
				title := strings.TrimSpace(closingHeadingPattern.ReplaceAllString(match[2], ""))
				base := headingID(title)
				counts[base]++
				id := base
				if counts[base] > 1 {
					id = fmt.Sprintf("%s-%d", base, counts[base])
				}
				headings = append(headings, markdownHeading{OutlineSection: OutlineSection{ID: id, Title: title, Level: len(match[1])}, start: offset})
			}
		}
		offset += len(line)
	}
	return headings
}

func markdownFence(line string) (byte, int, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	marker := line[0]
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	return marker, length, length >= 3
}

func markdownFenceCloses(line string, markerLength int) bool {
	return strings.TrimSpace(line[markerLength:]) == ""
}

func headingID(title string) string {
	var builder strings.Builder
	lastDash := false
	for _, runeValue := range strings.ToLower(title) {
		if (runeValue >= 'a' && runeValue <= 'z') || (runeValue >= '0' && runeValue <= '9') {
			builder.WriteRune(runeValue)
			lastDash = false
		} else if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result != "" {
		return result
	}
	digest := sha256.Sum256([]byte(title))
	return fmt.Sprintf("heading-%x", digest[:6])
}
