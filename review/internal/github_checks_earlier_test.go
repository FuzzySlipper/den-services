package review

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	earlierGatedSHA  = "0123456789abcdef0123456789abcdef01234567"
	earlierParentSHA = "1111111111111111111111111111111111111111"
	docsWorkflowPath = ".github/workflows/docs.yml"
	docsWorkflow     = `name: docs
on:
  workflow_dispatch:
  push:
    branches: [main]
    paths:
      - 'README.md'
      - 'docs/**'
`
)

// earlierFixture is a repository where the gated commit ran `verify-rust` and
// its parent ran `verify-docs` in docs.yml.
type earlierFixture struct {
	workflow      string
	changed       []map[string]string
	compareStatus string
	jobConclusion string
	gatedDocsRun  bool
	contentsCode  int
	olderPass     bool
}

func newEarlierFixture() earlierFixture {
	return earlierFixture{
		workflow: docsWorkflow, compareStatus: "ahead", jobConclusion: "success",
		changed: []map[string]string{{"filename": "crates/engine/src/lib.rs"}},
	}
}

func (f earlierFixture) serve(t *testing.T) *httptest.Server {
	t.Helper()
	runs := []map[string]any{{"id": 20, "head_sha": earlierGatedSHA, "path": ".github/workflows/rust.yml", "event": "push", "status": "completed", "conclusion": "success"}}
	if f.gatedDocsRun {
		runs = append(runs, map[string]any{"id": 21, "head_sha": earlierGatedSHA, "path": docsWorkflowPath, "event": "push", "status": "completed", "conclusion": "success"})
	}
	runs = append(runs, map[string]any{"id": 10, "head_sha": earlierParentSHA, "path": docsWorkflowPath, "event": "push", "status": "completed", "conclusion": f.jobConclusion})
	if f.olderPass {
		runs = append(runs, map[string]any{"id": 9, "head_sha": "2222222222222222222222222222222222222222", "path": docsWorkflowPath, "event": "push", "status": "completed", "conclusion": "success"})
	}
	write := func(w http.ResponseWriter, payload any) {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/commits/" + earlierGatedSHA + "/check-runs":
			write(w, map[string]any{"check_runs": []map[string]any{{"id": 1, "name": "verify-rust", "status": "completed", "conclusion": "success"}}})
		case "/repos/owner/repo/compare/" + earlierGatedSHA + "...main":
			write(w, map[string]any{"status": "identical"})
		case "/repos/owner/repo/actions/runs":
			write(w, map[string]any{"workflow_runs": runs})
		case "/repos/owner/repo/actions/runs/10/jobs":
			write(w, map[string]any{"jobs": []map[string]any{{"id": 100, "name": "verify-docs", "status": "completed", "conclusion": f.jobConclusion, "html_url": "https://github.test/docs"}}})
		case "/repos/owner/repo/actions/runs/9/jobs":
			write(w, map[string]any{"jobs": []map[string]any{{"id": 90, "name": "verify-docs", "status": "completed", "conclusion": "success"}}})
		case "/repos/owner/repo/actions/runs/20/jobs", "/repos/owner/repo/actions/runs/21/jobs":
			write(w, map[string]any{"jobs": []map[string]any{}})
		case "/repos/owner/repo/contents/" + docsWorkflowPath:
			if r.URL.Query().Get("ref") != earlierGatedSHA {
				t.Errorf("workflow read at ref %q", r.URL.Query().Get("ref"))
			}
			if f.contentsCode != 0 {
				w.WriteHeader(f.contentsCode)
				write(w, map[string]string{"message": "Not Found"})
				return
			}
			write(w, map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(f.workflow))})
		case "/repos/owner/repo/compare/" + earlierParentSHA + "..." + earlierGatedSHA:
			write(w, map[string]any{"status": f.compareStatus, "files": f.changed})
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func (f earlierFixture) check(t *testing.T) GitHubCheckResult {
	t.Helper()
	server := f.serve(t)
	defer server.Close()
	result, err := NewGitHubClient(server.URL, "", 0).CheckChange(context.Background(), GitHubChangeQuery{
		Repository: "owner/repo", CommitSHA: earlierGatedSHA, Ref: "main",
		RequiredChecks: []string{"verify-docs", "verify-rust"}, LaterCommitLimit: 3,
	})
	if err != nil {
		t.Fatalf("CheckChange() error = %v", err)
	}
	return result
}

func TestCheckChangeAcceptsEarlierPassWhenPathsFilterSkippedTheCommit(t *testing.T) {
	result := newEarlierFixture().check(t)
	if result.Status != GitHubCheckGateStatusPassed || len(result.MissingRequiredChecks) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Summary, "earlier commit") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if run := result.CheckRuns[0]; run.Name != "verify-docs" || run.HeadSHA != earlierParentSHA || run.Conclusion != "success" {
		t.Fatalf("check run = %+v", run)
	}
}

func TestCheckChangeKeepsCheckMissingWithoutProofOfTheSkip(t *testing.T) {
	tests := []struct {
		name   string
		change func(*earlierFixture)
	}{
		{"changed file matches the filter", func(f *earlierFixture) {
			f.changed = append(f.changed, map[string]string{"filename": "docs/guide.md"})
		}},
		{"renamed from a matching path", func(f *earlierFixture) {
			f.changed = []map[string]string{{"filename": "notes/guide.md", "previous_filename": "docs/guide.md"}}
		}},
		{"workflow file changed", func(f *earlierFixture) {
			f.changed = []map[string]string{{"filename": docsWorkflowPath}}
		}},
		{"workflow has no paths filter", func(f *earlierFixture) { f.workflow = "on:\n  push:\n    branches: [main]\n" }},
		{"workflow uses an unsupported pattern", func(f *earlierFixture) {
			f.workflow = "on:\n  push:\n    paths: ['docs/[ab]/**']\n"
		}},
		{"earlier commit is not an ancestor", func(f *earlierFixture) { f.compareStatus = "diverged" }},
		{"nearest run failed behind an older pass", func(f *earlierFixture) { f.jobConclusion, f.olderPass = "failure", true }},
		{"nearest job was skipped", func(f *earlierFixture) { f.jobConclusion = "skipped" }},
		{"workflow ran on the gated commit", func(f *earlierFixture) { f.gatedDocsRun = true }},
		{"workflow file is unreadable", func(f *earlierFixture) { f.contentsCode = http.StatusNotFound }},
		{"comparison may be cut short", func(f *earlierFixture) {
			f.changed = make([]map[string]string, compareFileLimit)
			for index := range f.changed {
				f.changed[index] = map[string]string{"filename": "src/lib.rs"}
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEarlierFixture()
			test.change(&fixture)
			result := fixture.check(t)
			if result.Status != GitHubCheckGateStatusPending || len(result.MissingRequiredChecks) != 1 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestParsePushPathFilter(t *testing.T) {
	tests := []struct {
		name      string
		workflow  string
		ok        bool
		triggers  []string
		untouched []string
	}{
		{
			name: "paths", workflow: docsWorkflow, ok: true,
			triggers: []string{"README.md", "docs/a/b.md"}, untouched: []string{"src/README.md", "docs", "README.md.bak"},
		},
		{
			name: "star stops at a slash", workflow: "on:\n  push:\n    paths: ['*.md', 'src/*/mod.rs']\n", ok: true,
			triggers: []string{"a.md", "src/x/mod.rs"}, untouched: []string{"docs/a.md", "src/x/y/mod.rs"},
		},
		{
			name: "double star", workflow: "on:\n  push:\n    paths: ['**/Cargo.toml', '**.js']\n", ok: true,
			triggers: []string{"Cargo.toml", "a/b/Cargo.toml", "a/b.js"}, untouched: []string{"a/Cargo.lock"},
		},
		{
			name: "negation in order", workflow: "on:\n  push:\n    paths: ['docs/**', '!docs/draft/**', 'docs/draft/keep.md']\n", ok: true,
			triggers: []string{"docs/a.md", "docs/draft/keep.md"}, untouched: []string{"docs/draft/b.md"},
		},
		{
			name: "paths-ignore", workflow: "on:\n  push:\n    paths-ignore: ['docs/**']\n", ok: true,
			triggers: []string{"src/lib.rs"}, untouched: []string{"docs/a.md"},
		},
		{name: "event name only", workflow: "on: push\n"},
		{name: "event list", workflow: "on: [push, pull_request]\n"},
		{name: "pull request filter only", workflow: "on:\n  pull_request:\n    paths: ['docs/**']\n  push:\n"},
		{name: "both filters", workflow: "on:\n  push:\n    paths: ['a']\n    paths-ignore: ['b']\n"},
		{name: "question mark", workflow: "on:\n  push:\n    paths: ['a?.md']\n"},
		{name: "directory pattern", workflow: "on:\n  push:\n    paths: ['docs/']\n"},
		{name: "not yaml", workflow: "on: [\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter, ok := parsePushPathFilter([]byte(test.workflow))
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			for _, file := range test.triggers {
				if !filter.triggeredBy(file) {
					t.Errorf("%q should trigger", file)
				}
			}
			for _, file := range test.untouched {
				if filter.triggeredBy(file) {
					t.Errorf("%q should not trigger", file)
				}
			}
		})
	}
}
