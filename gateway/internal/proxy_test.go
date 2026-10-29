package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"den-services/shared/health"
)

func TestGatewayProxiesRequestWithoutPayloadModification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.RequestURI() != "/api/messages?id=42" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}
		if string(body) != `{"hello":"den"}` {
			t.Fatalf("body = %s", string(body))
		}
		if r.Header.Get("X-Test-Header") != "kept" {
			t.Fatalf("X-Test-Header = %q, want kept", r.Header.Get("X-Test-Header"))
		}
		w.Header().Set("X-Upstream", "legacy")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer upstream.Close()

	server := newTestGatewayServer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodPost, "/api/messages?id=42", strings.NewReader(`{"hello":"den"}`))
	request.Header.Set("X-Test-Header", "kept")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if recorder.Header().Get("X-Upstream") != "legacy" {
		t.Fatalf("X-Upstream = %q, want legacy", recorder.Header().Get("X-Upstream"))
	}
	if recorder.Body.String() != "accepted" {
		t.Fatalf("body = %q, want accepted", recorder.Body.String())
	}
}

func TestGatewayRewritesConfiguredPathPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.RequestURI() != "/widgets/compare?include=details" {
			t.Fatalf("request uri = %s, want rewritten widget route", r.URL.RequestURI())
		}
		if string(body) != `{"left":{"id":"a"},"right":{"id":"b"}}` {
			t.Fatalf("body = %s, want unchanged comparison body", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Widget", "typed-error")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_widget","message":"right is invalid"}}`))
	}))
	defer upstream.Close()

	table, err := NewRouteTable([]routeFile{{
		Name:                 "widgets-browser",
		PathPattern:          "/v1/widgets",
		Methods:              []string{http.MethodGet, http.MethodPost},
		LegacyUpstreamURL:    upstream.URL,
		SuccessorUpstreamURL: upstream.URL,
		SuccessorMode:        string(SuccessorModeAlways),
		PathRewrite: pathRewriteFile{
			FromPrefix: "/v1/widgets",
			ToPrefix:   "/widgets",
		},
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table)
	request := httptest.NewRequest(http.MethodPost, "/v1/widgets/compare?include=details", strings.NewReader(`{"left":{"id":"a"},"right":{"id":"b"}}`))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	if recorder.Header().Get("X-Widget") != "typed-error" {
		t.Fatalf("X-Widget = %q, want typed-error", recorder.Header().Get("X-Widget"))
	}
	if recorder.Body.String() != `{"error":{"code":"invalid_widget","message":"right is invalid"}}` {
		t.Fatalf("body = %q, want typed upstream error", recorder.Body.String())
	}
}

func TestGatewayPassesClientAuthorizationHeaderThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer client-supplied" {
			t.Errorf("Authorization = %q, want client header passed through unchanged", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	server := newTestGatewayServer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	request.Header.Set("Authorization", "Bearer client-supplied")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestGatewayTranslatesIdentityOnlyForSuccessorRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if strings.Contains(string(body), "member_identity") {
			t.Fatalf("legacy identity leaked to successor body: %s", string(body))
		}
		if !strings.Contains(string(body), `"target_identity"`) {
			t.Fatalf("canonical identity missing from successor body: %s", string(body))
		}
		if !strings.Contains(string(body), `"profile":"pi-crew-planner"`) {
			t.Fatalf("canonical profile missing from successor body: %s", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	table, err := NewRouteTable([]routeFile{{
		Name:                 "delivery",
		PathPattern:          "/v1/delivery",
		LegacyUpstreamURL:    "http://127.0.0.1:1",
		SuccessorUpstreamURL: upstream.URL,
		IdentityTranslation: identityTranslationFile{
			Enabled: true,
			Targets: []identityTargetFile{{
				CanonicalField: "target_identity",
				Required:       true,
			}},
			Mappings: []identityMappingFile{{LegacyIdentity: "pi-crew-planner", Profile: "pi-crew-planner"}},
		},
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table)
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"pi-crew-planner","concrete_identity":"pi-crew-planner@den-srv"}`))
	request.Header.Set("X-Den-Migrated-Functions", "true")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestGatewayDoesNotTranslateLegacyPassThroughRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(body) != `{"member_identity":"pi-crew-planner"}` {
			t.Fatalf("body = %s, want untouched legacy identity", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	table, err := NewRouteTable([]routeFile{{
		Name:                 "delivery",
		PathPattern:          "/v1/delivery",
		LegacyUpstreamURL:    upstream.URL,
		SuccessorUpstreamURL: "http://127.0.0.1:1",
		IdentityTranslation: identityTranslationFile{
			Enabled: true,
			Targets: []identityTargetFile{{
				CanonicalField: "target_identity",
				Required:       true,
			}},
			Mappings: []identityMappingFile{{LegacyIdentity: "pi-crew-planner", Profile: "pi-crew-planner"}},
		},
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table)
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"pi-crew-planner"}`))
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestGatewayRejectsUnknownIdentityOnSuccessorRoute(t *testing.T) {
	table, err := NewRouteTable([]routeFile{{
		Name:                 "delivery",
		PathPattern:          "/v1/delivery",
		LegacyUpstreamURL:    "http://127.0.0.1:1",
		SuccessorUpstreamURL: "http://127.0.0.1:2",
		IdentityTranslation: identityTranslationFile{
			Enabled: true,
			Targets: []identityTargetFile{{
				CanonicalField: "target_identity",
				Required:       true,
			}},
			Mappings: []identityMappingFile{{LegacyIdentity: "pi-crew-planner", Profile: "pi-crew-planner"}},
		},
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table)
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"unknown","concrete_identity":"unknown@host"}`))
	request.Header.Set("X-Den-Migrated-Functions", "true")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestGatewayLiveRouteFamilyMix(t *testing.T) {
	var deliveryRequests []string
	delivery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/delivery/intents" {
			if strings.Contains(string(body), "member_identity") {
				t.Fatalf("legacy identity leaked to delivery create body: %s", string(body))
			}
			if !strings.Contains(string(body), `"target_identity"`) {
				t.Fatalf("delivery create body missing target_identity: %s", string(body))
			}
		}
		deliveryRequests = append(deliveryRequests, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer delivery.Close()

	var observationRequests []string
	observation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observationRequests = append(observationRequests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer observation.Close()

	var legacyRequests []string
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyRequests = append(legacyRequests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer legacy.Close()

	table, err := NewRouteTable(liveRouteFamilyTestRoutes(legacy.URL, delivery.URL, observation.URL))
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table)

	assertGatewayStatus(t, server, http.MethodGet, "/v1/delivery/intents?limit=1", "", "", http.StatusTeapot)
	assertGatewayStatus(t, server, http.MethodGet, "/v1/delivery/intents?limit=1", "false", "", http.StatusTeapot)
	assertGatewayStatus(t, server, http.MethodGet, "/v1/delivery/intents?limit=1", "true", "", http.StatusOK)
	assertGatewayStatus(t, server, http.MethodPost, "/v1/delivery/intents", "true", `{"member_identity":"pi-crew-planner","concrete_identity":"pi-crew-planner@den-srv","idempotency_key":"wake:pi-crew-planner:test"}`, http.StatusOK)
	assertGatewayStatus(t, server, http.MethodPost, "/v1/delivery/intents/42/claim", "true", `{"claim_token":"claim","claimed_by":{"profile":"pi-crew-planner","instance_id":"pi-crew-planner@den-srv"}}`, http.StatusOK)
	assertGatewayStatus(t, server, http.MethodGet, "/v1/observation/lane", "", "", http.StatusOK)
	assertGatewayStatus(t, server, http.MethodPost, "/v1/observation/activity-events", "", `{}`, http.StatusOK)
	assertGatewayStatus(t, server, http.MethodGet, "/api/messages", "", "", http.StatusTeapot)

	if got := strings.Join(deliveryRequests, ","); got != "GET /v1/delivery/intents?limit=1,POST /v1/delivery/intents,POST /v1/delivery/intents/42/claim" {
		t.Fatalf("delivery requests = %s", got)
	}
	if got := strings.Join(observationRequests, ","); got != "GET /v1/observation/lane,POST /v1/observation/activity-events" {
		t.Fatalf("observation requests = %s", got)
	}
	if got := strings.Join(legacyRequests, ","); got != "GET /v1/delivery/intents,GET /v1/delivery/intents,GET /api/messages" {
		t.Fatalf("legacy requests = %s", got)
	}
}

func TestGatewayHealthAndVersionArePublic(t *testing.T) {
	server := newTestGatewayServer(t, "http://127.0.0.1:1")
	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		server.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func assertGatewayStatus(t *testing.T, handler http.Handler, method string, target string, migrated string, body string, wantStatus int) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if migrated != "" {
		request.Header.Set("X-Den-Migrated-Functions", migrated)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, target, recorder.Code, wantStatus, recorder.Body.String())
	}
}

func liveRouteFamilyTestRoutes(legacyURL string, deliveryURL string, observationURL string) []routeFile {
	return []routeFile{
		{
			Name:                 "delivery-successor-canary-child-canonical",
			PathPattern:          "/v1/delivery/intents/",
			Methods:              []string{http.MethodGet, http.MethodPost},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: deliveryURL,
		},
		{
			Name:                 "delivery-successor-canary-list-canonical",
			PathPattern:          "/v1/delivery/intents",
			Methods:              []string{http.MethodGet},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: deliveryURL,
		},
		{
			Name:                 "delivery-successor-canary",
			PathPattern:          "/v1/delivery",
			Methods:              []string{http.MethodPost},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: deliveryURL,
			IdentityTranslation: identityTranslationFile{
				Enabled: true,
				Targets: []identityTargetFile{{
					CanonicalField: "target_identity",
					Required:       true,
					ProfileFields:  []string{"member_identity", "agent_identity", "profile_identity", "profile"},
					InstanceFields: []string{"agent_instance_id", "concrete_identity", "instance_id"},
					SessionFields:  []string{"session_key", "hermes_session_key", "session_id"},
				}},
				Mappings: []identityMappingFile{{LegacyIdentity: "pi-crew-planner", Profile: "pi-crew-planner"}},
			},
		},
		{
			Name:                 "observation-activity-writes",
			PathPattern:          "/v1/observation/activity-events",
			Methods:              []string{http.MethodPost},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: observationURL,
			SuccessorMode:        string(SuccessorModeAlways),
		},
		{
			Name:                 "observation-lifecycle-writes",
			PathPattern:          "/v1/observation/lifecycle-events",
			Methods:              []string{http.MethodPost},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: observationURL,
			SuccessorMode:        string(SuccessorModeAlways),
		},
		{
			Name:                 "observation-reads",
			PathPattern:          "/v1/observation",
			Methods:              []string{http.MethodGet},
			LegacyUpstreamURL:    legacyURL,
			SuccessorUpstreamURL: observationURL,
			SuccessorMode:        string(SuccessorModeAlways),
		},
		{Name: "legacy-den-channels-all", PathPattern: "/", LegacyUpstreamURL: legacyURL},
	}
}

func newTestGatewayServer(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()
	table, err := NewRouteTable([]routeFile{{Name: "all", PathPattern: "/", LegacyUpstreamURL: upstreamURL}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	return newTestGatewayServerWithRoutes(t, table)
}

func newTestGatewayServerWithRoutes(t *testing.T, table *RouteTable) http.Handler {
	t.Helper()
	info, err := health.NewBuildInfo("gateway", "test", "testcommit", fixedBuiltAt())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&Config{
		BindAddr:          "127.0.0.1:0",
		RoutingConfigPath: "routes.yaml",
		HTTP:              HTTPConfig{ReadHeaderTimeout: testTimeout()},
	}, table, info, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server.Handler
}
