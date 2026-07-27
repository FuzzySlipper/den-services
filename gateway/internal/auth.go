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
	bearerToken            string
	additionalBearerTokens []string
}

type callerAuthFile struct {
	BearerToken  string   `yaml:"bearer_token"`
	BearerTokens []string `yaml:"bearer_tokens"`
}

func newCallerAuth(file callerAuthFile, values sharedconfig.Values) (CallerAuth, error) {
	rawTokens := make([]string, 0, 1+len(file.BearerTokens))
	if rawToken := strings.TrimSpace(file.BearerToken); rawToken != "" {
		rawTokens = append(rawTokens, rawToken)
	}
	for _, rawToken := range file.BearerTokens {
		if rawToken = strings.TrimSpace(rawToken); rawToken != "" {
			rawTokens = append(rawTokens, rawToken)
		}
	}
	if len(rawTokens) == 0 {
		return CallerAuth{}, nil
	}

	tokens := make([]string, 0, len(rawTokens))
	seen := make(map[string]struct{}, len(rawTokens))
	for _, rawToken := range rawTokens {
		token, err := values.Expand(rawToken)
		if err != nil {
			return CallerAuth{}, fmt.Errorf("expanding caller_auth bearer token: %w", err)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return CallerAuth{}, errors.New("caller_auth bearer tokens must not be empty")
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return CallerAuth{
		bearerToken:            tokens[0],
		additionalBearerTokens: tokens[1:],
	}, nil
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
	authorized := subtle.ConstantTimeCompare([]byte(token), []byte(a.bearerToken))
	for _, candidate := range a.additionalBearerTokens {
		authorized |= subtle.ConstantTimeCompare([]byte(token), []byte(candidate))
	}
	return authorized == 1
}
