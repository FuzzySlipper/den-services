package gateway

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"den-services/shared/api"
)

type ProxyHandler struct {
	routes *RouteTable
	logger *slog.Logger
}

func NewProxyHandler(routes *RouteTable, logger *slog.Logger) *ProxyHandler {
	return &ProxyHandler{
		routes: routes,
		logger: logger,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	match, ok := h.routes.Match(r.URL.Path, preferSuccessor(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	request := r
	if match.UsesSuccessor && match.IdentityTranslation.Enabled() {
		translated, err := translatedRequest(r, match.IdentityTranslation)
		if err != nil {
			if errors.Is(err, ErrIdentityTranslationFailed) {
				api.WriteError(w, http.StatusBadRequest, "identity_translation_failed", err.Error())
				return
			}
			api.WriteServiceError(w, err)
			return
		}
		request = translated
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			rewriteProxyRequest(request, match.Target)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error("gateway proxy failed", "method", r.Method, "path", r.URL.Path, "error", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, request)
}

func preferSuccessor(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(migratedFunctionsHeader)), "true")
}

func rewriteProxyRequest(request *httputil.ProxyRequest, target *url.URL) {
	request.Out.URL.Scheme = target.Scheme
	request.Out.URL.Host = target.Host
	request.Out.URL.Path = joinPaths(target.Path, request.In.URL.Path)
	request.Out.URL.RawPath = ""
	request.Out.URL.RawQuery = request.In.URL.RawQuery
	request.Out.Host = target.Host
}

func translatedRequest(r *http.Request, translation IdentityTranslation) (*http.Request, error) {
	if r.Body == nil {
		return r, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if err := r.Body.Close(); err != nil {
		return nil, err
	}
	translatedBody, err := translation.TranslateJSON(body)
	if err != nil {
		return nil, err
	}
	translated := r.Clone(r.Context())
	translated.Body = io.NopCloser(bytes.NewReader(translatedBody))
	translated.ContentLength = int64(len(translatedBody))
	return translated, nil
}

func joinPaths(left string, right string) string {
	if left == "" || left == "/" {
		return right
	}
	if right == "" || right == "/" {
		return left
	}
	return strings.TrimRight(left, "/") + "/" + strings.TrimLeft(right, "/")
}
