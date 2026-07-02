package librarian

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQueryReturnsBoundedMixedSources(t *testing.T) {
	taskID := int64(7)
	fake := &fakeSources{
		task: TaskDetail{Task: TaskSummary{ID: taskID, ProjectID: "den-services", Title: "Build librarian service", Description: "Route query_librarian off Core"}},
		messages: []MessageSummary{{
			ID: 11, ProjectID: "den-services", TaskID: &taskID, Sender: "planner", Content: "Use bounded retrieval with citations.",
		}},
		documents: map[string][]DocumentSearchResult{
			"den-services": {{ProjectID: "den-services", Slug: "librarian-contract", Title: "Librarian Contract", Summary: "Bounded retrieval", Snippet: "query_librarian returns relevant items."}},
			"_global":      {{ProjectID: "_global", Slug: "global-search", Title: "Global Search", Summary: "Include global docs", Snippet: "Global documents can provide background."}},
		},
		knowledge: []KnowledgeSearchResult{{Slug: "source-citations", Title: "Source Citations", Summary: "Cite successor services", Snippet: "Every answer should cite source ids."}},
	}
	service := NewService(fake, SourceLimits{Tasks: 2, Messages: 2, Documents: 2, Knowledge: 2})

	result, err := service.Query(context.Background(), "den-services", QueryRequest{Query: "librarian retrieval citations", TaskID: &taskID})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if result.ProjectID != "den-services" || result.Confidence == ConfidenceLow {
		t.Fatalf("result = %#v", result)
	}
	if len(result.RelevantItems) != 5 {
		t.Fatalf("relevant_items len = %d, want 5", len(result.RelevantItems))
	}
	if result.RelevantItems[0].Type != ItemTypeTask || result.RelevantItems[0].SourceID != "7" {
		t.Fatalf("first item = %#v, want task citation", result.RelevantItems[0])
	}
	if !fake.searchedGlobal {
		t.Fatal("global documents were not searched")
	}
}

func TestQueryRejectsTaskProjectMismatch(t *testing.T) {
	taskID := int64(9)
	service := NewService(&fakeSources{
		task: TaskDetail{Task: TaskSummary{ID: taskID, ProjectID: "other", Title: "Wrong scope"}},
	}, SourceLimits{})

	_, err := service.Query(context.Background(), "den-services", QueryRequest{Query: "scope", TaskID: &taskID})
	if err == nil || !strings.Contains(err.Error(), ErrProjectMismatch.Error()) {
		t.Fatalf("Query() error = %v, want project mismatch", err)
	}
}

func TestQueryDegradesOptionalSourceOutages(t *testing.T) {
	service := NewService(&fakeSources{messagesErr: errors.New("messages down")}, SourceLimits{})

	result, err := service.Query(context.Background(), "den-services", QueryRequest{Query: "deployment"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Source != SourceMessages {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if result.Confidence == ConfidenceLow {
		t.Fatalf("confidence = %q, want partial confidence", result.Confidence)
	}
}

type fakeSources struct {
	task           TaskDetail
	tasks          []TaskSummary
	messages       []MessageSummary
	documents      map[string][]DocumentSearchResult
	knowledge      []KnowledgeSearchResult
	messagesErr    error
	searchedGlobal bool
}

func (f *fakeSources) ValidateProject(context.Context, string) error {
	return nil
}

func (f *fakeSources) GetTask(_ context.Context, _ string, _ int64) (TaskDetail, error) {
	return f.task, nil
}

func (f *fakeSources) ListTasks(_ context.Context, projectID string, _ int) ([]TaskSummary, error) {
	if len(f.tasks) > 0 {
		return f.tasks, nil
	}
	return []TaskSummary{{ID: 1, ProjectID: projectID, Title: "Deploy service", Description: "Deployment notes"}}, nil
}

func (f *fakeSources) ListMessages(context.Context, string, *int64, int) ([]MessageSummary, error) {
	if f.messagesErr != nil {
		return nil, f.messagesErr
	}
	return f.messages, nil
}

func (f *fakeSources) SearchDocuments(_ context.Context, projectID string, _ string, _ int) ([]DocumentSearchResult, error) {
	if projectID == GlobalProjectID {
		f.searchedGlobal = true
	}
	return f.documents[projectID], nil
}

func (f *fakeSources) SearchKnowledge(context.Context, string, int) ([]KnowledgeSearchResult, error) {
	return f.knowledge, nil
}
