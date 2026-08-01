package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestDeployableRouteExampleCoversBrowserOwners(t *testing.T) {
	tokenNames := []string{
		"DEN_GATEWAY_WEB_TOKEN",
		"DEN_GATEWAY_PROJECTS_UPSTREAM_TOKEN",
		"DEN_GATEWAY_TASKS_UPSTREAM_TOKEN",
		"DEN_GATEWAY_MESSAGES_UPSTREAM_TOKEN",
		"DEN_GATEWAY_DOCUMENTS_UPSTREAM_TOKEN",
		"DEN_GATEWAY_GUIDANCE_UPSTREAM_TOKEN",
		"DEN_GATEWAY_REVIEW_UPSTREAM_TOKEN",
		"DEN_GATEWAY_ARTIFACTS_UPSTREAM_TOKEN",
		"DEN_GATEWAY_VISUAL_CONTRACT_UPSTREAM_TOKEN",
		"DEN_GATEWAY_LIBRARIAN_UPSTREAM_TOKEN",
		"DEN_GATEWAY_DELIVERY_WRITE_TOKEN",
		"DEN_GATEWAY_DELIVERY_UPSTREAM_TOKEN",
		"DEN_GATEWAY_OBSERVATION_WRITE_TOKEN",
		"DEN_GATEWAY_OBSERVATION_READ_TOKEN",
		"DEN_GATEWAY_OBSERVATION_UPSTREAM_TOKEN",
		"DEN_GATEWAY_CONVERSATION_WRITE_TOKEN",
		"DEN_GATEWAY_CONVERSATION_READ_TOKEN",
		"DEN_GATEWAY_CONVERSATION_UPSTREAM_TOKEN",
		"DEN_GATEWAY_TIMELINE_READ_TOKEN",
		"DEN_GATEWAY_TIMELINE_UPSTREAM_TOKEN",
		"DEN_GATEWAY_DOC_PUBLISH_CALLER_TOKEN",
		"DEN_GATEWAY_DOC_PUBLISH_UPSTREAM_TOKEN",
		"DEN_GATEWAY_RUNTIME_CALLER_TOKEN",
		"DEN_GATEWAY_RUNTIME_UPSTREAM_TOKEN",
	}
	for _, name := range tokenNames {
		t.Setenv(name, name+"-value")
	}

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
		{"GET", "/v1/projects/den-web/agent-guidance/entries", "127.0.0.1:8097"},
		{"POST", "/v1/projects/den-web/tasks/42/review/request", "127.0.0.1:8096"},
		{"GET", "/v1/artifacts/12/content", "127.0.0.1:8090"},
		{"POST", "/v1/visual-contracts/compare", "127.0.0.1:8086"},
		{"POST", "/v1/projects/den-web/librarian/query", "127.0.0.1:8098"},
		{"GET", "/v1/conversation/channels", "127.0.0.1:8084"},
		{"GET", "/v1/timeline/projects/den-web/stream", "127.0.0.1:8085"},
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
		if testCase.path == "/v1/visual-contracts/compare" {
			if got := match.PathRewrite.Apply(testCase.path); got != "/visual-contracts/compare" {
				t.Errorf("visual-contract rewritten path = %s, want /visual-contracts/compare", got)
			}
		}
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		request.Header.Set("Authorization", "Bearer DEN_GATEWAY_WEB_TOKEN-value")
		if !match.CallerAuth.Authorizes(request) {
			t.Errorf("Match(%s, %s) does not authorize the web-edge token", testCase.method, testCase.path)
		}
	}
}
