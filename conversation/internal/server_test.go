package conversation

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestHealthAndVersionArePublic(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		server.Handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func TestConversationAPIRequiresAuth(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/conversation/channels", nil)
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestConversationAPIAuthenticatedListChannels(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/conversation/channels", nil)
	request.Header.Set("Authorization", "Bearer conversation-token")
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var channels []ChannelResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &channels); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("len(channels) = %d, want 0", len(channels))
	}
}

func TestConversationAPIAppendMessageRequiresIdempotencyKey(t *testing.T) {
	server := newTestServer(t)
	body := []byte(`{
		"sender_type": "human",
		"sender_identity": "patchfoot",
		"body": "hello",
		"message_kind": "human_text",
		"source_kind": "conversation"
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/conversation/channels/1/messages", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer conversation-token")
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestConversationAPIPilotLifecycle(t *testing.T) {
	server := newTestServer(t)
	channel := postJSON[ChannelResponse](t, server, http.MethodPost, "/v1/conversation/channels", `{
		"slug": "pilot",
		"display_name": "Pilot",
		"kind": "project_default",
		"project_id": "den-services",
		"created_by": "den-system",
		"visibility": "normal"
	}`, nil, http.StatusCreated)
	if channel.ID == 0 {
		t.Fatal("channel ID is zero")
	}
	gotChannel := postJSON[ChannelResponse](t, server, http.MethodGet, "/v1/conversation/channels/1", "", nil, http.StatusOK)
	if gotChannel.ID != channel.ID {
		t.Fatalf("got channel ID = %d, want %d", gotChannel.ID, channel.ID)
	}
	defaultChannel := postJSON[ChannelResponse](t, server, http.MethodPut, "/v1/conversation/projects/den-services/default-channel", `{
		"slug": "den-services",
		"display_name": "den-services",
		"created_by": "den-system"
	}`, nil, http.StatusOK)
	if defaultChannel.ProjectID == nil || *defaultChannel.ProjectID != "den-services" {
		t.Fatalf("default project ID = %v, want den-services", defaultChannel.ProjectID)
	}
	message := postJSON[MessageResponse](t, server, http.MethodPost, "/v1/conversation/channels/1/messages", `{
		"sender_type": "human",
		"sender_identity": "patchfoot",
		"body": "hello",
		"message_kind": "human_text",
		"source_kind": "conversation"
	}`, map[string]string{idempotencyHeader: "message-1"}, http.StatusCreated)
	if message.ID == 0 {
		t.Fatal("message ID is zero")
	}
	messages := postJSON[[]MessageResponse](t, server, http.MethodGet, "/v1/conversation/channels/1/messages?limit=10", "", nil, http.StatusOK)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	membership := postJSON[MembershipResponse](t, server, http.MethodPut, "/v1/conversation/channels/1/memberships", `{
		"member_type": "human",
		"member_identity": "patchfoot",
		"membership_status": "active",
		"wake_policy": "never",
		"membership_purpose": "ordinary"
	}`, nil, http.StatusOK)
	if membership.MemberIdentity != "patchfoot" {
		t.Fatalf("membership identity = %s, want patchfoot", membership.MemberIdentity)
	}
	memberships := postJSON[[]MembershipResponse](t, server, http.MethodGet, "/v1/conversation/memberships?member_identity=patchfoot&limit=10", "", nil, http.StatusOK)
	if len(memberships) != 1 {
		t.Fatalf("len(memberships) = %d, want 1", len(memberships))
	}
	reaction := postJSON[ReactionResponse](t, server, http.MethodPost, "/v1/conversation/messages/1/reactions", `{
		"reactor_type": "human",
		"reactor_identity": "patchfoot",
		"reaction": "ack"
	}`, nil, http.StatusCreated)
	if reaction.MessageID != message.ID {
		t.Fatalf("reaction message ID = %d, want %d", reaction.MessageID, message.ID)
	}
	cursor := postJSON[ReadCursorResponse](t, server, http.MethodPut, "/v1/conversation/channels/1/read-cursors", `{
		"reader_type": "human",
		"reader_identity": "patchfoot",
		"last_read_message_id": 1
	}`, nil, http.StatusOK)
	if cursor.ReaderIdentity != "patchfoot" {
		t.Fatalf("cursor reader = %s, want patchfoot", cursor.ReaderIdentity)
	}
	cursors := postJSON[[]ReadCursorResponse](t, server, http.MethodGet, "/v1/conversation/channels/1/read-cursors", "", nil, http.StatusOK)
	if len(cursors) != 1 {
		t.Fatalf("len(cursors) = %d, want 1", len(cursors))
	}
}

func TestConversationAPIMembershipIncludesWakeTarget(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/instances" {
			t.Fatalf("runtime path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Fatalf("runtime auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"instance_id": "den-mcp-runner@den-k8plus",
			"profile_identity": "den-mcp-runner",
			"state": "active",
			"started_at": "2026-07-05T01:00:00Z",
			"last_heartbeat_at": "2026-07-05T01:05:00Z"
		}]`))
	}))
	t.Cleanup(runtimeServer.Close)
	server := newTestServerWithConfig(t, &Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://channels.example/denservices",
		ServiceToken: "conversation-token",
		DefaultLimit: 100,
		MaxLimit:     500,
		WakeTargets: WakeTargetsConfig{
			RuntimeBaseURL:      runtimeServer.URL,
			RuntimeServiceToken: "runtime-token",
			Timeout:             time.Second,
			Enabled:             true,
		},
		HTTP: HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	})

	membership := postJSON[MembershipResponse](t, server, http.MethodPut, "/v1/conversation/channels/1/memberships", `{
		"member_type": "agent",
		"member_identity": "den-mcp-runner",
		"profile_identity": "den-mcp-runner",
		"membership_status": "active",
		"wake_policy": "mentions_only",
		"membership_purpose": "ordinary"
	}`, nil, http.StatusOK)

	if membership.WakeTarget == nil {
		t.Fatal("WakeTarget is nil")
	}
	if membership.WakeTarget.Profile.String() != "den-mcp-runner" || membership.WakeTarget.InstanceID.String() != "den-mcp-runner@den-k8plus" {
		t.Fatalf("WakeTarget = %+v", membership.WakeTarget)
	}
}

func newTestServer(t *testing.T) *http.Server {
	t.Helper()
	return newTestServerWithConfig(t, &Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://channels.example/denservices",
		ServiceToken: "conversation-token",
		DefaultLimit: 100,
		MaxLimit:     500,
		HTTP:         HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	})
}

func newTestServerWithConfig(t *testing.T, cfg *Config) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("conversation", "test", "testcommit", fixedBuiltAt())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServerWithStore(cfg, info, newMemoryConversationStore(t))
	if err != nil {
		t.Fatalf("NewHTTPServerWithStore() error = %v", err)
	}
	return server
}

func fixedBuiltAt() time.Time {
	return time.Date(2026, 6, 20, 2, 0, 0, 0, time.UTC)
}

func postJSON[T any](t *testing.T, server *http.Server, method string, path string, body string, headers map[string]string, wantStatus int) T {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Authorization", "Bearer conversation-token")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	var response T
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(%s %s) error = %v; body=%s", method, path, err, recorder.Body.String())
	}
	return response
}
