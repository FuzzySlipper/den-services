package backend

import (
	"testing"
)

func TestReviewPipelineRouteUsesProjectAndPagination(t *testing.T) {
	limit := 25
	offset := 50
	got, err := reviewRESTURL("http://review.test", Route{
		Operation: "list_review_pipeline",
		Path:      "/v1/projects/{project_id}/review/pipeline",
	}, reviewToolArguments{ProjectID: "rusty-view", Limit: &limit, Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	want := "http://review.test/v1/projects/rusty-view/review/pipeline?limit=25&offset=50"
	if got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}
