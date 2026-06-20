package timeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestReadHandlerResponseShapeAndLimitValidation(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{testMessageItem(10, now)}}
	server := testServer(t, store, now)

	req := httptest.NewRequest(http.MethodGet, "/v1/timeline/channels/123/items?limit=1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response TimelineResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if response.Scope.Kind != "channel" || response.Scope.ChannelID == nil || *response.Scope.ChannelID != 123 {
		t.Fatalf("scope = %+v, want channel 123", response.Scope)
	}
	if len(response.Items) != 1 || response.Items[0].TimelineID != "msg:10" || response.NextCursor == nil {
		t.Fatalf("response items/next_cursor = %+v / %v", response.Items, response.NextCursor)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/v1/timeline/channels/123/items?limit=99", nil)
	badReq.Header.Set("Authorization", "Bearer test-token")
	badRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(badRecorder, badReq)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d, want 400", badRecorder.Code)
	}
}

func TestProjectReadHandlerPassesProjectScope(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{testMessageItem(10, now)}}
	server := testServer(t, store, now)

	req := httptest.NewRequest(http.MethodGet, "/v1/timeline/projects/den-services/items", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if store.query.Scope.Kind != ScopeKindProject || store.query.Scope.ProjectID == nil || *store.query.Scope.ProjectID != "den-services" {
		t.Fatalf("store scope = %+v, want project den-services", store.query.Scope)
	}
}

func TestSSEStreamSendsOpenItemAndHeartbeat(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{testMessageItem(10, now)}}
	server := testServer(t, store, now)
	testHTTPServer := httptest.NewServer(server.Handler)
	defer testHTTPServer.Close()

	req, err := http.NewRequest(http.MethodGet, testHTTPServer.URL+"/v1/timeline/channels/123/stream?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	response, err := testHTTPServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	buffer := make([]byte, 1024)
	var builder strings.Builder
	deadline := time.After(500 * time.Millisecond)
	for !strings.Contains(builder.String(), "event: heartbeat") {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for heartbeat; stream so far:\n%s", builder.String())
		default:
			n, readErr := response.Body.Read(buffer)
			if n > 0 {
				builder.Write(buffer[:n])
			}
			if readErr != nil {
				t.Fatalf("reading stream error = %v", readErr)
			}
		}
	}
	body := builder.String()
	for _, fragment := range []string{"event: stream_open", "event: timeline_item", `"timeline_id":"msg:10"`, "event: heartbeat"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("stream missing %q:\n%s", fragment, body)
		}
	}
	if count := strings.Count(body, `"timeline_id":"msg:10"`); count != 1 {
		t.Fatalf("stream emitted msg:10 %d times, want 1:\n%s", count, body)
	}
}

func testServer(t *testing.T, store TimelineStore, now time.Time) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("timeline", "dev", "unknown", now)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewHTTPServerWithStore(testConfig(), info, store)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
