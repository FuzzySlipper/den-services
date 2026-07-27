package gateway

import (
	"net/http/httptest"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestCallerAuthAcceptsPrimaryAndAdditionalBearerTokens(t *testing.T) {
	auth, err := newCallerAuth(callerAuthFile{
		BearerToken:  "${PRIMARY_TOKEN}",
		BearerTokens: []string{"${WEB_TOKEN}", "${PRIMARY_TOKEN}"},
	}, sharedconfig.FromMap(map[string]string{
		"PRIMARY_TOKEN": "primary-token",
		"WEB_TOKEN":     "web-token",
	}))
	if err != nil {
		t.Fatalf("newCallerAuth() error = %v", err)
	}

	for _, token := range []string{"primary-token", "web-token"} {
		request := httptest.NewRequest("GET", "/", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		if !auth.Authorizes(request) {
			t.Fatalf("Authorizes() = false for configured token %q", token)
		}
	}

	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer wrong-token")
	if auth.Authorizes(request) {
		t.Fatal("Authorizes() = true for an unknown token")
	}
}
