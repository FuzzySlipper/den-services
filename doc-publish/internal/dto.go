package docpublish

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
	if requireSource {
		if r.Source.DocumentProjectID == "" {
			return invalidRequest("source.document_project_id is required")
		}
		if r.Source.DocumentSlug == "" {
			return invalidRequest("source.document_slug is required")
		}
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
	Source          DocumentSource    `json:"source"`
}
