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
