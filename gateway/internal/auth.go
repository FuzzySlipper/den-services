package gateway

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sharedconfig "den-services/shared/config"
)

const defaultAuthHeader = "Authorization"

type CallerAuth struct {
	bearerToken string
}

type callerAuthFile struct {
	BearerToken string `yaml:"bearer_token"`
}

func newCallerAuth(file callerAuthFile, values sharedconfig.Values) (CallerAuth, error) {
	rawToken := strings.TrimSpace(file.BearerToken)
	if rawToken == "" {
		return CallerAuth{}, nil
	}
	token, err := values.Expand(rawToken)
	if err != nil {
		return CallerAuth{}, fmt.Errorf("expanding caller_auth.bearer_token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return CallerAuth{}, errors.New("caller_auth.bearer_token must not be empty")
	}
	return CallerAuth{bearerToken: token}, nil
}

func (a CallerAuth) Enabled() bool {
	return a.bearerToken != ""
}

func (a CallerAuth) Authorizes(r *http.Request) bool {
	if !a.Enabled() {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(defaultAuthHeader))
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.bearerToken)) == 1
}
