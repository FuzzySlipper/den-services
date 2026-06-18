package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"den-services/shared/identity"
)

func TestNewLoggerAttachesServiceMetadata(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewLogger(&buffer, Config{
		Level:   slog.LevelInfo,
		Service: "gateway",
		Version: "1.2.3",
	})

	logger.Info("ready")

	got := buffer.String()
	if !strings.Contains(got, `"service":"gateway"`) || !strings.Contains(got, `"version":"1.2.3"`) {
		t.Fatalf("log output = %s", got)
	}
}

func TestFromHTTPRequestAttachesRequestAndIdentity(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewLogger(&buffer, Config{})
	session := identity.SessionKey("sess-1")
	agent := identity.AgentIdentity{
		Profile:    identity.ProfileIdentity("planner"),
		InstanceID: identity.AgentInstanceID("planner@host"),
		Session:    &session,
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "req-1")

	requestLogger := FromHTTPRequest(logger, request, &agent)
	requestLogger.Info("handled")

	got := buffer.String()
	for _, want := range []string{`"request_id":"req-1"`, `"profile":"planner"`, `"instance_id":"planner@host"`, `"session_key":"sess-1"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output = %s, missing %s", got, want)
		}
	}
}
