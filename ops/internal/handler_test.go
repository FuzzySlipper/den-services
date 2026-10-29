package ops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestAPI(t *testing.T, now time.Time) *httptest.Server {
	t.Helper()
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), &fixedClock{now: now})
	mux := http.NewServeMux()
	NewHandler(service, 50).RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func call(t *testing.T, method string, url string, body []byte, wantStatus int, target any) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		var payload bytes.Buffer
		_, _ = payload.ReadFrom(response.Body)
		t.Fatalf("%s %s = %d, want %d: %s", method, url, response.StatusCode, wantStatus, payload.String())
	}
	if target != nil {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			t.Fatalf("decoding %s %s: %v", method, url, err)
		}
	}
}

func TestHTTPReportLifecycle(t *testing.T) {
	server := newTestAPI(t, utc("2026-10-29T05:00:00Z"))
	job := []byte(`{"host_id":"den-agents","engine":"borg","kind":"backup","target":"den-nas:/mnt/storage/backup/den-agents/borg",
		"schedule":{"timezone":"America/Los_Angeles","times":["07:00"],"grace":"3h"}}`)
	var registered JobStatusResponse
	call(t, http.MethodPut, server.URL+"/v1/ops/jobs/den-agents-git", job, http.StatusOK, &registered)
	if registered.Status != string(StatusNeverReported) || registered.NextDueAt == nil {
		t.Fatalf("registered = %+v", registered)
	}

	// A host can post its report file unchanged; unknown fields are kept.
	raw, err := os.ReadFile(filepath.Join("testdata", "borg-backup-success.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reported ReportRunResponse
	call(t, http.MethodPost, server.URL+"/v1/ops/reports", raw, http.StatusOK, &reported)
	if reported.Outcome != string(OutcomeCreated) || reported.Job.Status != string(StatusOK) || reported.Job.LastSuccess == nil {
		t.Fatalf("reported = %+v", reported)
	}
	call(t, http.MethodPut, server.URL+"/v1/ops/jobs/den-agents-git/runs/"+reported.Run.RunID, raw, http.StatusOK, &reported)
	if reported.Outcome != string(OutcomeDuplicate) {
		t.Fatalf("redelivery outcome = %s", reported.Outcome)
	}

	var detail RunDetailResponse
	call(t, http.MethodGet, server.URL+"/v1/ops/jobs/den-agents-git/runs/"+reported.Run.RunID, nil, http.StatusOK, &detail)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(detail.Report, &original); err != nil || original["directories_scanned"] == nil {
		t.Fatalf("stored report lost fields: %v", err)
	}

	var summary SummaryResponse
	call(t, http.MethodGet, server.URL+"/v1/ops/summary", nil, http.StatusOK, &summary)
	if summary.Status != string(StatusOK) || summary.Counts[string(StatusOK)] != 1 || len(summary.Hosts) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	var errorsResponse ErrorsResponse
	call(t, http.MethodGet, server.URL+"/v1/ops/errors", nil, http.StatusOK, &errorsResponse)
	if len(errorsResponse.Problems) != 0 {
		t.Fatalf("errors = %+v", errorsResponse)
	}
}

func TestHTTPErrors(t *testing.T) {
	server := newTestAPI(t, utc("2026-10-29T05:00:00Z"))
	call(t, http.MethodGet, server.URL+"/v1/ops/jobs/missing", nil, http.StatusNotFound, nil)
	call(t, http.MethodGet, server.URL+"/v1/ops/jobs/missing/runs/r", nil, http.StatusNotFound, nil)
	call(t, http.MethodPut, server.URL+"/v1/ops/jobs/j", []byte(`{"host_id":"h","engine":"borg","kind":"backup","surprise":1}`), http.StatusBadRequest, nil)
	call(t, http.MethodPut, server.URL+"/v1/ops/jobs/j", []byte(`{"host_id":"h","engine":"borg","kind":"backup","schedule":{"timezone":"UTC","times":["7"],"grace":"1h"}}`), http.StatusBadRequest, nil)
	call(t, http.MethodPost, server.URL+"/v1/ops/reports", []byte(`not json`), http.StatusBadRequest, nil)
	call(t, http.MethodGet, server.URL+"/v1/ops/summary?limit=0", nil, http.StatusOK, nil)
	call(t, http.MethodGet, server.URL+"/v1/ops/errors?limit=0", nil, http.StatusBadRequest, nil)
}
