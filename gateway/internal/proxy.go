package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
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
	target, ok := h.routes.Match(r.URL.Path, preferSuccessor(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			rewriteProxyRequest(request, target)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error("gateway proxy failed", "method", r.Method, "path", r.URL.Path, "error", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
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

func joinPaths(left string, right string) string {
	if left == "" || left == "/" {
		return right
	}
	if right == "" || right == "/" {
		return left
	}
	return strings.TrimRight(left, "/") + "/" + strings.TrimLeft(right, "/")
}
