package webedge

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

func newGatewayProxy(cfg GatewayConfig, logger *slog.Logger) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = cfg.ResponseHeaderTimeout
	transport.DialContext = (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(cfg.BaseURL)
			request.Out.URL.Path = strings.TrimPrefix(request.In.URL.Path, "/api")
			request.Out.URL.RawPath = ""
			request.SetXForwarded()
			request.Out.Header.Del("Authorization")
			request.Out.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("gateway proxy failed", "method", r.Method, "path", r.URL.Path, "error", err)
			writeJSONError(w, http.StatusBadGateway, "gateway_unavailable", "Den Gateway is unavailable")
		},
	}
	return proxy
}
