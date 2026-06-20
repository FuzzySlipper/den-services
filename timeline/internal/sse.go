package timeline

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type streamQuery struct {
	Scope        TimelineScope
	After        string
	Limit        int
	IncludeDebug bool
}

func streamTimeline(w http.ResponseWriter, r *http.Request, service *Service, config *Config, query streamQuery) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		apiErr := codedBadRequest("streaming_unsupported", "streaming unsupported")
		http.Error(w, apiErr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	currentCursor := stringPtrFromNonEmpty(query.After)
	if err := writeSSE(w, "stream_open", streamOpenResponse{
		Scope:           toScopeResponse(query.Scope),
		Cursor:          currentCursor,
		SupportedEvents: []string{"timeline_item", "timeline_refresh", "heartbeat"},
		HeartbeatMS:     config.Stream.HeartbeatInterval.Milliseconds(),
	}); err != nil {
		return
	}
	flusher.Flush()

	pollTicker := time.NewTicker(config.Stream.PollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(config.Stream.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		if err := emitTimelineItems(r, w, service, query, &currentCursor); err != nil {
			_ = writeSSE(w, "stream_error", streamErrorResponse{Error: err.Error()})
			flusher.Flush()
			return
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-pollTicker.C:
			continue
		case <-heartbeatTicker.C:
			_ = writeSSE(w, "heartbeat", heartbeatResponse{
				Now:    service.clock().UTC(),
				Cursor: currentCursor,
			})
			flusher.Flush()
		}
	}
}

func emitTimelineItems(
	r *http.Request,
	w http.ResponseWriter,
	service *Service,
	query streamQuery,
	currentCursor **string,
) error {
	after := ""
	if *currentCursor != nil {
		after = **currentCursor
	}
	response, err := service.ListItems(r.Context(), query.Scope, after, query.Limit, query.IncludeDebug)
	if err != nil {
		return err
	}
	for _, item := range response.Items {
		if err := writeSSE(w, "timeline_item", item); err != nil {
			return err
		}
	}
	*currentCursor = response.NextCursor
	return nil
}

func writeSSE(w http.ResponseWriter, eventName string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding sse payload: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func stringPtrFromNonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
