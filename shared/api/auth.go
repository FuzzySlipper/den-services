package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

const defaultAuthHeader = "Authorization"

type ServiceTokenAuth struct {
	token      string
	headerName string
}

func NewServiceTokenAuth(token string) (*ServiceTokenAuth, error) {
	return NewServiceTokenAuthWithHeader(token, defaultAuthHeader)
}

func NewServiceTokenAuthWithHeader(token string, headerName string) (*ServiceTokenAuth, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrMissingServiceToken
	}
	if strings.TrimSpace(headerName) == "" {
		return nil, ErrMissingAuthHeader
	}
	return &ServiceTokenAuth{
		token:      token,
		headerName: headerName,
	}, nil
}

func (a *ServiceTokenAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			WriteServiceError(w, ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *ServiceTokenAuth) authorized(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get(a.headerName))
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) == 1
}

var (
	ErrMissingServiceToken = errors.New("service token is required") //nolint:gochecknoglobals
	ErrMissingAuthHeader   = errors.New("auth header is required")   //nolint:gochecknoglobals
)
