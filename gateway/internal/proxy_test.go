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
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("Authorization = %q, want caller token preserved on legacy route", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-Upstream", "legacy")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer upstream.Close()

	server := newTestGatewayServer(t, upstream.URL, "token")
	request := httptest.NewRequest(http.MethodPost, "/api/messages?id=42", strings.NewReader(`{"hello":"den"}`))
	request.Header.Set("Authorization", "Bearer token")
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
		if r.Header.Get("Authorization") != "Bearer upstream-token" {
			t.Fatalf("Authorization = %q, want upstream token", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	table, err := NewRouteTable([]routeFile{{
		Name:                 "delivery",
		PathPattern:          "/v1/delivery",
		LegacyUpstreamURL:    "http://127.0.0.1:1",
		SuccessorUpstreamURL: upstream.URL,
		SuccessorAuth:        upstreamAuthFile{BearerToken: "upstream-token"},
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
	server := newTestGatewayServerWithRoutes(t, table, "token")
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"pi-crew-planner","concrete_identity":"pi-crew-planner@den-srv"}`))
	request.Header.Set("Authorization", "Bearer token")
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
		SuccessorAuth:        testSuccessorAuth(),
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
	server := newTestGatewayServerWithRoutes(t, table, "token")
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"pi-crew-planner"}`))
	request.Header.Set("Authorization", "Bearer token")
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
		SuccessorAuth:        testSuccessorAuth(),
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
	server := newTestGatewayServerWithRoutes(t, table, "token")
	request := httptest.NewRequest(http.MethodPost, "/v1/delivery/intents", strings.NewReader(`{"member_identity":"unknown","concrete_identity":"unknown@host"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Den-Migrated-Functions", "true")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestGatewayObservationRoutesUseSeparateReadAndWriteAuth(t *testing.T) {
	var upstreamRequests []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer observation-upstream-token" {
			t.Fatalf("Authorization = %q, want observation upstream token", r.Header.Get("Authorization"))
		}
		upstreamRequests = append(upstreamRequests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	table, err := NewRouteTable([]routeFile{
		{
			Name:                 "observation-write-activity",
			PathPattern:          "/v1/observation/activity-events",
			Methods:              []string{http.MethodPost},
			LegacyUpstreamURL:    "http://127.0.0.1:1",
			SuccessorUpstreamURL: upstream.URL,
			SuccessorMode:        string(SuccessorModeAlways),
			SuccessorAuth:        upstreamAuthFile{BearerToken: "observation-upstream-token"},
			CallerAuth:           callerAuthFile{BearerToken: "observation-write-token"},
		},
		{
			Name:                 "observation-write-lifecycle",
			PathPattern:          "/v1/observation/lifecycle-events",
			Methods:              []string{http.MethodPost},
			LegacyUpstreamURL:    "http://127.0.0.1:1",
			SuccessorUpstreamURL: upstream.URL,
			SuccessorMode:        string(SuccessorModeAlways),
			SuccessorAuth:        upstreamAuthFile{BearerToken: "observation-upstream-token"},
			CallerAuth:           callerAuthFile{BearerToken: "observation-write-token"},
		},
		{
			Name:                 "observation-read",
			PathPattern:          "/v1/observation",
			Methods:              []string{http.MethodGet},
			LegacyUpstreamURL:    "http://127.0.0.1:1",
			SuccessorUpstreamURL: upstream.URL,
			SuccessorMode:        string(SuccessorModeAlways),
			SuccessorAuth:        upstreamAuthFile{BearerToken: "observation-upstream-token"},
			CallerAuth:           callerAuthFile{BearerToken: "observation-read-token"},
		},
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	server := newTestGatewayServerWithRoutes(t, table, "gateway-default-token")

	readRequest := httptest.NewRequest(http.MethodGet, "/v1/observation/lane", nil)
	readRequest.Header.Set("Authorization", "Bearer observation-read-token")
	readRecorder := httptest.NewRecorder()
	server.ServeHTTP(readRecorder, readRequest)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("read status = %d, want %d body=%s", readRecorder.Code, http.StatusOK, readRecorder.Body.String())
	}

	writeRequest := httptest.NewRequest(http.MethodPost, "/v1/observation/activity-events", strings.NewReader(`{}`))
	writeRequest.Header.Set("Authorization", "Bearer observation-write-token")
	writeRecorder := httptest.NewRecorder()
	server.ServeHTTP(writeRecorder, writeRequest)
	if writeRecorder.Code != http.StatusOK {
		t.Fatalf("write status = %d, want %d body=%s", writeRecorder.Code, http.StatusOK, writeRecorder.Body.String())
	}

	blockedRequest := httptest.NewRequest(http.MethodPost, "/v1/observation/activity-events", strings.NewReader(`{}`))
	blockedRequest.Header.Set("Authorization", "Bearer observation-read-token")
	blockedRecorder := httptest.NewRecorder()
	server.ServeHTTP(blockedRecorder, blockedRequest)
	if blockedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("blocked status = %d, want %d body=%s", blockedRecorder.Code, http.StatusUnauthorized, blockedRecorder.Body.String())
	}

	if got := strings.Join(upstreamRequests, ","); got != "GET /v1/observation/lane,POST /v1/observation/activity-events" {
		t.Fatalf("upstream requests = %s", got)
	}
}

func TestGatewayRejectsUnauthenticatedProxyRequest(t *testing.T) {
	server := newTestGatewayServer(t, "http://127.0.0.1:1", "token")
	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGatewayHealthAndVersionArePublic(t *testing.T) {
	server := newTestGatewayServer(t, "http://127.0.0.1:1", "token")
	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		server.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func newTestGatewayServer(t *testing.T, upstreamURL string, token string) http.Handler {
	t.Helper()
	table, err := NewRouteTable([]routeFile{{Name: "all", PathPattern: "/", LegacyUpstreamURL: upstreamURL}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	return newTestGatewayServerWithRoutes(t, table, token)
}

func newTestGatewayServerWithRoutes(t *testing.T, table *RouteTable, token string) http.Handler {
	t.Helper()
	info, err := health.NewBuildInfo("gateway", "test", "testcommit", fixedBuiltAt())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&Config{
		BindAddr:          "127.0.0.1:0",
		RoutingConfigPath: "routes.yaml",
		ServiceToken:      token,
		HTTP:              HTTPConfig{ReadHeaderTimeout: testTimeout()},
	}, table, info, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server.Handler
}
