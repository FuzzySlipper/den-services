package logging

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"den-services/shared/identity"
)

const requestIDHeader = "X-Request-ID"

type Config struct {
	Level   slog.Level
	Service string
	Version string
}

type RequestContext struct {
	RequestID       string
	ServiceIdentity *identity.AgentIdentity
}

func NewLogger(writer io.Writer, cfg Config) *slog.Logger {
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: cfg.Level})
	logger := slog.New(handler)
	if cfg.Service != "" {
		logger = logger.With("service", cfg.Service)
	}
	if cfg.Version != "" {
		logger = logger.With("version", cfg.Version)
	}
	return logger
}

func WithRequest(logger *slog.Logger, request RequestContext) *slog.Logger {
	if request.RequestID != "" {
		logger = logger.With("request_id", request.RequestID)
	}
	if request.ServiceIdentity != nil && request.ServiceIdentity.IsValid() {
		logger = logger.With(
			"profile", request.ServiceIdentity.Profile.String(),
			"instance_id", request.ServiceIdentity.InstanceID.String(),
		)
		if request.ServiceIdentity.Session != nil {
			logger = logger.With("session_key", request.ServiceIdentity.Session.String())
		}
	}
	return logger
}

func FromHTTPRequest(logger *slog.Logger, r *http.Request, serviceIdentity *identity.AgentIdentity) *slog.Logger {
	requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
	return WithRequest(logger, RequestContext{
		RequestID:       requestID,
		ServiceIdentity: serviceIdentity,
	})
}
