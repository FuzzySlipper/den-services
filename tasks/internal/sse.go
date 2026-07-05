package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type taskChangeStreamer interface {
	ListTaskChanges(ctx context.Context, projectID string, afterID int64, limit int) ([]TaskChangeEvent, error)
}

type taskChangeStreamQuery struct {
	ProjectID string
	AfterID   int64
	Limit     int
}

type taskChangeStreamOpenResponse struct {
	ProjectID       string   `json:"project_id"`
	Cursor          string   `json:"cursor,omitempty"`
	SupportedEvents []string `json:"supported_events"`
	HeartbeatMS     int64    `json:"heartbeat_ms"`
	BackfillURL     string   `json:"backfill_url"`
}

type taskChangeHeartbeatResponse struct {
	Now    time.Time `json:"now"`
	Cursor string    `json:"cursor,omitempty"`
}

type taskChangeStreamErrorResponse struct {
	Error string `json:"error"`
}

func streamTaskChanges(w http.ResponseWriter, r *http.Request, service taskChangeStreamer, config *Config, query taskChangeStreamQuery) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	currentCursor := taskChangeCursor(query.AfterID)
	backfillURL := taskChangesBackfillURL(query.ProjectID, currentCursor)
	if err := writeTaskChangeSSE(w, "", "stream_open", taskChangeStreamOpenResponse{
		ProjectID:       query.ProjectID,
		Cursor:          currentCursor,
		SupportedEvents: []string{"task_change", "heartbeat"},
		HeartbeatMS:     config.Stream.HeartbeatInterval.Milliseconds(),
		BackfillURL:     backfillURL,
	}); err != nil {
		return
	}
	flusher.Flush()

	pollTicker := time.NewTicker(config.Stream.PollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(config.Stream.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		if err := emitTaskChanges(r, w, service, query, &currentCursor); err != nil {
			_ = writeTaskChangeSSE(w, "", "stream_error", taskChangeStreamErrorResponse{Error: err.Error()})
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
			_ = writeTaskChangeSSE(w, currentCursor, "heartbeat", taskChangeHeartbeatResponse{
				Now:    time.Now().UTC(),
				Cursor: currentCursor,
			})
			flusher.Flush()
		}
	}
}

func emitTaskChanges(
	r *http.Request,
	w http.ResponseWriter,
	service taskChangeStreamer,
	query taskChangeStreamQuery,
	currentCursor *string,
) error {
	afterID, err := parseTaskChangeCursor(*currentCursor)
	if err != nil {
		return err
	}
	events, err := service.ListTaskChanges(r.Context(), query.ProjectID, afterID, query.Limit)
	if err != nil {
		return err
	}
	for _, event := range events {
		response := toTaskChangeResponse(event)
		response.BackfillURL = taskChangesBackfillURL(query.ProjectID, response.Cursor)
		response.ReconnectURL = taskChangesStreamURL(query.ProjectID, response.Cursor)
		if err := writeTaskChangeSSE(w, response.Cursor, "task_change", response); err != nil {
			return err
		}
		*currentCursor = response.Cursor
	}
	return nil
}

func writeTaskChangeSSE(w http.ResponseWriter, eventID string, eventName string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding sse payload: %w", err)
	}
	if eventID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", eventID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func taskChangesBackfillURL(projectID string, cursor string) string {
	if cursor == "" {
		return "/v1/projects/" + projectID + "/tasks/changes"
	}
	return "/v1/projects/" + projectID + "/tasks/changes?after=" + cursor
}

func taskChangesStreamURL(projectID string, cursor string) string {
	if cursor == "" {
		return "/v1/projects/" + projectID + "/tasks/changes/stream"
	}
	return "/v1/projects/" + projectID + "/tasks/changes/stream?after=" + cursor
}

func parseTaskChangeCursor(cursor string) (int64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("cursor must be non-negative")
	}
	return value, nil
}
