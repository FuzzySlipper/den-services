package librarian

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type SourceClients interface {
	ValidateProject(ctx context.Context, projectID string) error
	GetTask(ctx context.Context, projectID string, taskID int64) (TaskDetail, error)
	ListTasks(ctx context.Context, projectID string, limit int) ([]TaskSummary, error)
	ListMessages(ctx context.Context, projectID string, taskID *int64, limit int) ([]MessageSummary, error)
	SearchDocuments(ctx context.Context, projectID string, query string, limit int) ([]DocumentSearchResult, error)
	SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeSearchResult, error)
}

type Service struct {
	clients       SourceClients
	defaultBudget SourceLimits
}

func NewService(clients SourceClients, defaultBudget SourceLimits) *Service {
	return &Service{clients: clients, defaultBudget: defaultBudget.withDefaults()}
}

func (s *Service) Query(ctx context.Context, projectID string, req QueryRequest) (QueryResponse, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		projectID = strings.TrimSpace(req.ProjectID)
	}
	if projectID == "" {
		return QueryResponse{}, validationFailed(ErrMissingProjectID)
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return QueryResponse{}, validationFailed(ErrMissingQuery)
	}
	if err := s.clients.ValidateProject(ctx, projectID); err != nil {
		return QueryResponse{}, err
	}

	budget := req.SourceLimits.merged(s.defaultBudget)
	includeGlobal := true
	if req.IncludeGlobal != nil {
		includeGlobal = *req.IncludeGlobal
	}
	terms := extractTerms(query)
	candidates := make([]Candidate, 0, budget.Tasks+budget.Messages+budget.Documents+budget.Knowledge)
	warnings := make([]Warning, 0)

	if req.TaskID != nil {
		task, err := s.clients.GetTask(ctx, projectID, *req.TaskID)
		if err != nil {
			return QueryResponse{}, err
		}
		if task.Task.ProjectID != projectID {
			return QueryResponse{}, validationFailed(fmt.Errorf("%w: task %d belongs to %s", ErrProjectMismatch, *req.TaskID, task.Task.ProjectID))
		}
		candidates = append(candidates, taskCandidate(task.Task, true, terms))
	} else {
		tasks, err := s.clients.ListTasks(ctx, projectID, budget.Tasks)
		if err != nil {
			warnings = append(warnings, sourceUnavailable(SourceTasks, err))
		} else {
			for _, task := range tasks {
				candidates = append(candidates, taskCandidate(task, false, terms))
			}
		}
	}

	messages, err := s.clients.ListMessages(ctx, projectID, req.TaskID, budget.Messages)
	if err != nil {
		warnings = append(warnings, sourceUnavailable(SourceMessages, err))
	} else {
		for _, message := range messages {
			candidates = append(candidates, messageCandidate(message, terms))
		}
	}

	documents, err := s.clients.SearchDocuments(ctx, projectID, query, budget.Documents)
	if err != nil {
		warnings = append(warnings, sourceUnavailable(SourceDocuments, err))
	} else {
		for _, document := range documents {
			candidates = append(candidates, documentCandidate(document, terms))
		}
	}
	if includeGlobal && projectID != GlobalProjectID {
		globalDocs, err := s.clients.SearchDocuments(ctx, GlobalProjectID, query, budget.Documents)
		if err != nil {
			warnings = append(warnings, sourceUnavailable(SourceDocuments, fmt.Errorf("%s: %w", GlobalProjectID, err)))
		} else {
			for _, document := range globalDocs {
				candidates = append(candidates, documentCandidate(document, terms))
			}
		}
	}

	knowledge, err := s.clients.SearchKnowledge(ctx, query, budget.Knowledge)
	if err != nil {
		warnings = append(warnings, sourceUnavailable(SourceKnowledge, err))
	} else {
		for _, entry := range knowledge {
			candidates = append(candidates, knowledgeCandidate(entry, terms))
		}
	}

	items := rankCandidates(candidates, terms, budget)
	relevant := make([]RelevantItem, 0, len(items))
	for _, candidate := range items {
		relevant = append(relevant, candidate.toRelevantItem(terms))
	}
	return QueryResponse{
		Query:           query,
		ProjectID:       projectID,
		TaskID:          req.TaskID,
		RelevantItems:   relevant,
		Recommendations: recommendations(relevant, warnings),
		Confidence:      confidence(relevant, warnings),
		Warnings:        warnings,
		Budget:          budget,
	}, nil
}

func taskCandidate(task TaskSummary, exact bool, terms []string) Candidate {
	text := strings.TrimSpace(task.Title + "\n" + task.Description + "\n" + strings.Join(task.Tags, " "))
	score := scoreText(text, terms) + 2
	if exact {
		score += 6
	}
	return Candidate{
		ItemType:  ItemTypeTask,
		Source:    SourceTasks,
		SourceID:  strconv.FormatInt(task.ID, 10),
		ProjectID: task.ProjectID,
		Title:     task.Title,
		Summary:   firstNonEmpty(task.Description, fmt.Sprintf("Task %d is %s.", task.ID, task.Status)),
		Snippet:   excerpt(text, terms, 360),
		Text:      text,
		Score:     score,
	}
}

func messageCandidate(message MessageSummary, terms []string) Candidate {
	text := strings.TrimSpace(message.Sender + "\n" + message.Intent + "\n" + message.Content)
	return Candidate{
		ItemType:  ItemTypeMessage,
		Source:    SourceMessages,
		SourceID:  strconv.FormatInt(message.ID, 10),
		ProjectID: message.ProjectID,
		Title:     firstNonEmpty(message.Sender, "message"),
		Summary:   excerpt(message.Content, terms, 180),
		Snippet:   excerpt(text, terms, 360),
		Text:      text,
		Score:     scoreText(text, terms) + 1,
	}
}

func documentCandidate(document DocumentSearchResult, terms []string) Candidate {
	text := strings.TrimSpace(document.Title + "\n" + document.Summary + "\n" + document.Snippet)
	return Candidate{
		ItemType:  ItemTypeDocument,
		Source:    SourceDocuments,
		SourceID:  document.Slug,
		ProjectID: document.ProjectID,
		Title:     document.Title,
		Summary:   firstNonEmpty(document.Summary, document.Title),
		Snippet:   excerpt(text, terms, 420),
		Text:      text,
		Score:     scoreText(text, terms) + document.Rank + 3,
	}
}

func knowledgeCandidate(entry KnowledgeSearchResult, terms []string) Candidate {
	text := strings.TrimSpace(entry.Title + "\n" + entry.Summary + "\n" + entry.Snippet)
	return Candidate{
		ItemType: ItemTypeKnowledge, Source: SourceKnowledge,
		SourceID: entry.Slug,
		Title:    entry.Title,
		Summary:  firstNonEmpty(entry.Summary, entry.Title),
		Snippet:  excerpt(text, terms, 420),
		Text:     text,
		Score:    scoreText(text, terms) + entry.Rank + 2,
	}
}

func rankCandidates(candidates []Candidate, terms []string, budget SourceLimits) []Candidate {
	seen := make(map[string]bool, len(candidates))
	deduped := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.Source + ":" + candidate.ProjectID + ":" + candidate.SourceID
		if seen[key] {
			continue
		}
		seen[key] = true
		if candidate.Score == 0 {
			candidate.Score = scoreText(candidate.Text, terms)
		}
		deduped = append(deduped, candidate)
	}
	sort.SliceStable(deduped, func(i int, j int) bool {
		if deduped[i].Score == deduped[j].Score {
			return deduped[i].Source < deduped[j].Source
		}
		return deduped[i].Score > deduped[j].Score
	})
	limit := budget.Tasks + budget.Messages + budget.Documents + budget.Knowledge
	if limit > 12 {
		limit = 12
	}
	if len(deduped) > limit {
		return deduped[:limit]
	}
	return deduped
}

func recommendations(items []RelevantItem, warnings []Warning) []string {
	if len(items) == 0 {
		if len(warnings) > 0 {
			return []string{"Some sources were unavailable; retry once those services are healthy."}
		}
		return []string{"No matching context was found; broaden the query or check source-specific tools."}
	}
	recs := []string{"Review the cited sources before making changes."}
	if len(items) >= 3 {
		recs = append(recs, "Start with the highest-scored task, document, and knowledge citations for cross-checking.")
	}
	if len(warnings) > 0 {
		recs = append(recs, "Treat this as partial context because at least one source was unavailable.")
	}
	return recs
}

func confidence(items []RelevantItem, warnings []Warning) string {
	if len(items) == 0 {
		return ConfidenceLow
	}
	if len(warnings) > 0 {
		return ConfidenceMedium
	}
	if len(items) >= 4 {
		return ConfidenceHigh
	}
	return ConfidenceMedium
}

func scoreText(text string, terms []string) float64 {
	lower := strings.ToLower(text)
	score := 0.0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score += 1
		}
	}
	return score
}

func extractTerms(query string) []string {
	seen := make(map[string]bool)
	terms := make([]string, 0)
	for _, token := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(token) < 3 || seen[token] {
			continue
		}
		seen[token] = true
		terms = append(terms, token)
	}
	return terms
}

func excerpt(text string, terms []string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	lower := strings.ToLower(text)
	start := 0
	for _, term := range terms {
		if index := strings.Index(lower, term); index >= 0 {
			start = max(0, index-limit/4)
			break
		}
	}
	end := min(len(text), start+limit)
	snippet := strings.TrimSpace(text[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(text) {
		snippet += "..."
	}
	return snippet
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
