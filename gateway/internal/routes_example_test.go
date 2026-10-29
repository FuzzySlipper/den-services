package gateway

import "testing"

func TestDeployableRouteExampleCoversBrowserOwners(t *testing.T) {
	table, err := LoadRouteTable("../config/routes.example.yaml")
	if err != nil {
		t.Fatalf("LoadRouteTable() error = %v", err)
	}

	cases := []struct {
		method string
		path   string
		host   string
	}{
		{"GET", "/v1/projects", "127.0.0.1:8091"},
		{"PATCH", "/v1/projects/den-web/tasks/42", "127.0.0.1:8092"},
		{"GET", "/v1/projects/den-web/messages", "127.0.0.1:8093"},
		{"POST", "/v1/projects/den-web/documents", "127.0.0.1:8094"},
		{"GET", "/v1/knowledge/entries", "127.0.0.1:8095"},
		{"GET", "/v1/knowledge/entries/global-boundaries", "127.0.0.1:8095"},
		{"GET", "/v1/projects/den-web/agent-guidance/entries", "127.0.0.1:8097"},
		{"POST", "/v1/projects/den-web/tasks/42/review/request", "127.0.0.1:8096"},
		{"GET", "/v1/projects/den-web/tasks/42/manual-review", "127.0.0.1:8413"},
		{"POST", "/v1/projects/den-web/tasks/42/manual-review", "127.0.0.1:8413"},
		{"GET", "/v1/artifacts/12/content", "127.0.0.1:8090"},
		{"POST", "/v1/projects/den-web/librarian/query", "127.0.0.1:8098"},
		{"GET", "/v1/handoffs", "127.0.0.1:8099"},
		{"GET", "/v1/projects/den-web/board/posts", "127.0.0.1:8100"},
		{"DELETE", "/v1/board/comments/12", "127.0.0.1:8100"},
		{"GET", "/v1/timeline/projects/den-web/stream", "127.0.0.1:8085"},
		{"POST", "/v1/delivery/intents", "127.0.0.1:8083"},
		{"GET", "/v1/observation/lane", "127.0.0.1:8082"},
		{"POST", "/v1/observation/activity-events", "127.0.0.1:8082"},
		{"POST", "/v1/blog/publications", "127.0.0.1:8087"},
		{"POST", "/v1/runtime/instances/agent-1/heartbeat", "127.0.0.1:8081"},
		{"GET", "/v1/ops/summary", "127.0.0.1:8102"},
		{"GET", "/v1/ops/jobs/den-agents-git/runs/20261028T212037-2462905", "127.0.0.1:8102"},
		{"PUT", "/v1/ops/jobs/den-agents-git/runs/20261028T212037-2462905", "127.0.0.1:8102"},
		{"POST", "/v1/ops/reports", "127.0.0.1:8102"},
	}
	for _, testCase := range cases {
		match, ok := table.Match(testCase.method, testCase.path, false)
		if !ok {
			t.Errorf("Match(%s, %s) did not find a route", testCase.method, testCase.path)
			continue
		}
		if match.Target.Host != testCase.host {
			t.Errorf("Match(%s, %s) target = %s, want %s", testCase.method, testCase.path, match.Target.Host, testCase.host)
		}
	}
}
