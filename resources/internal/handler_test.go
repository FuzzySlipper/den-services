package resources

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func call(t *testing.T, method string, url string, body string, wantStatus int, target any) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
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

func TestHTTPCheckInLifecycle(t *testing.T) {
	service, _ := newTestService(t)
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	base := server.URL + "/v1/resources"

	var receipt WorkReceipt
	call(t, http.MethodPost, base+"/work", `{"host":"den-m5","title":"Generate","owner":"me","resources":["m5-comfy"],"ttl_seconds":60}`, http.StatusOK, &receipt)
	if receipt.State != "running" || receipt.Warnings == nil {
		t.Fatalf("receipt = %+v", receipt)
	}
	var row WorkResponse
	call(t, http.MethodPost, base+"/work/"+receipt.ID, `{"state":"completed","note":"done"}`, http.StatusOK, &row)
	if row.State != "completed" || row.Note != "done" {
		t.Fatalf("checked out = %+v", row)
	}
	call(t, http.MethodPost, base+"/work/nope", `{}`, http.StatusNotFound, nil)
	call(t, http.MethodPost, base+"/work", `{"host":"den-m5","bogus":1}`, http.StatusBadRequest, nil)
	call(t, http.MethodPost, base+"/work", `{"host":"den-x"}`, http.StatusNotFound, nil)

	var active WorkListResponse
	call(t, http.MethodGet, base+"/work?active=true", "", http.StatusOK, &active)
	if len(active.Work) != 0 {
		t.Fatalf("active = %+v", active)
	}
	call(t, http.MethodGet, base+"/work?limit=0", "", http.StatusBadRequest, nil)

	var resource ResourceResponse
	call(t, http.MethodPost, base+"/directory", `{"id":"m5-comfy","touch":true,"owner":"me"}`, http.StatusOK, &resource)
	if resource.LastUsedBy != "me" {
		t.Fatalf("touched = %+v", resource)
	}
	call(t, http.MethodGet, base+"/directory/missing", "", http.StatusNotFound, nil)
	call(t, http.MethodPut, base+"/hosts/den-m5", `{"name":"den-m5","role":"Strix Halo","position":1}`, http.StatusOK, nil)
	call(t, http.MethodPost, base+"/hosts/den-m5/telemetry", `{"telemetry":{"sampled_at":1,"gpus":[{"utilization_percent":"high"}]}}`, http.StatusBadRequest, nil)
	call(t, http.MethodPost, base+"/hosts/den-m5/apps", `{"apps":[{"name":"ComfyUI","kind":"comfy","online":true,"running":0,"queued":1}]}`, http.StatusOK, nil)
	call(t, http.MethodPut, base+"/settings", `{"poll_seconds":20}`, http.StatusOK, nil)

	var state StateResponse
	call(t, http.MethodGet, base+"/state", "", http.StatusOK, &state)
	if len(state.Hosts) != 2 || state.Hosts[0].Role != "Strix Halo" || len(state.Hosts[0].Apps) != 1 || state.PollSeconds != 20 || len(state.Resources) != 2 {
		t.Fatalf("state = %+v", state)
	}
}
