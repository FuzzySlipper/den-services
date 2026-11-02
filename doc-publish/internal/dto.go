package docpublish

import (
	"path"
	"strings"
)

type PublicationRequest struct {
	Source      DocumentSource     `json:"source"`
	Options     PublicationOptions `json:"options,omitempty"`
	RequestedBy string             `json:"requested_by"`
	Document    *SourceDocument    `json:"document,omitempty"`
}

func (r PublicationRequest) Validate(requireSource bool) error {
	if r.RequestedBy == "" {
		return invalidRequest("requested_by is required")
	}
	if !requireSource {
		return nil
	}
	if r.Source.Kind() == SourceKindWiki {
		if r.Source.DocumentProjectID != "" || r.Source.DocumentSlug != "" || r.Source.ProjectID != "" {
			return invalidRequest("source names a wiki page and a document; give one")
		}
		if r.Document != nil {
			return invalidRequest("a wiki page source is always fetched from den-wiki; omit document")
		}
		return validateWikiPage(r.Source.WikiPage)
	}
	if r.Source.DocumentProjectID == "" {
		return invalidRequest("source.document_project_id is required")
	}
	if r.Source.DocumentSlug == "" {
		return invalidRequest("source.document_slug is required")
	}
	return nil
}

// validateWikiPage accepts a clean den-wiki page path under wiki/.
func validateWikiPage(page string) error {
	if !strings.HasPrefix(page, "wiki/") || !strings.HasSuffix(page, ".md") || path.Clean(page) != page || strings.Contains(page, "..") {
		return invalidRequest("source.wiki_page must be a wiki/ page path such as wiki/agent-memory.md")
	}
	return nil
}

type PublicationResponse struct {
	PublicationID   string            `json:"publication_id"`
	Status          PublicationStatus `json:"status"`
	DryRun          bool              `json:"dry_run"`
	Title           string            `json:"title"`
	Slug            string            `json:"slug"`
	PostPath        string            `json:"post_path"`
	PublicURL       string            `json:"public_url"`
	GitCommit       string            `json:"git_commit,omitempty"`
	PreviewMarkdown string            `json:"preview_markdown,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
	AssetPaths      []string          `json:"asset_paths,omitempty"`
	Source          DocumentSource    `json:"source"`
}
