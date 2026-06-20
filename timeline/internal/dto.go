package timeline

import (
	"encoding/json"
	"strconv"
	"time"
)

type TimelineScopeResponse struct {
	Kind      string  `json:"kind"`
	ChannelID *int64  `json:"channel_id,omitempty"`
	ProjectID *string `json:"project_id,omitempty"`
}

type TimelineResponse struct {
	Scope      TimelineScopeResponse  `json:"scope"`
	Items      []TimelineItemResponse `json:"items"`
	NextCursor *string                `json:"next_cursor"`
	SnapshotAt time.Time              `json:"snapshot_at"`
}

type TimelineItemResponse struct {
	TimelineID   string                 `json:"timeline_id"`
	Cursor       string                 `json:"cursor"`
	OccurredAt   time.Time              `json:"occurred_at"`
	SourceDomain string                 `json:"source_domain"`
	SourceID     string                 `json:"source_id"`
	EventKind    string                 `json:"event_kind"`
	RenderKind   string                 `json:"render_kind"`
	DisplayOnly  bool                   `json:"display_only"`
	ChannelID    *int64                 `json:"channel_id"`
	ProjectID    *string                `json:"project_id"`
	TaskID       *int64                 `json:"task_id"`
	Actor        TimelineActorResponse  `json:"actor"`
	Body         *string                `json:"body"`
	Summary      *string                `json:"summary"`
	Severity     string                 `json:"severity"`
	Metadata     json.RawMessage        `json:"metadata"`
	SourceRef    TimelineSourceResponse `json:"source_ref"`
}

type TimelineActorResponse struct {
	Type            string  `json:"type"`
	Identity        string  `json:"identity"`
	ProfileIdentity *string `json:"profile_identity"`
	AgentInstanceID *string `json:"agent_instance_id"`
}

type TimelineSourceResponse struct {
	Domain string `json:"domain"`
	Table  string `json:"table"`
	ID     string `json:"id"`
}

type streamOpenResponse struct {
	Scope           TimelineScopeResponse `json:"scope"`
	Cursor          *string               `json:"cursor"`
	SupportedEvents []string              `json:"supported_events"`
	HeartbeatMS     int64                 `json:"heartbeat_ms"`
}

type heartbeatResponse struct {
	Now    time.Time `json:"now"`
	Cursor *string   `json:"cursor"`
}

type streamErrorResponse struct {
	Error string `json:"error"`
}

func toTimelineResponse(scope TimelineScope, items []TimelineItem, snapshotAt time.Time) (TimelineResponse, error) {
	responses := make([]TimelineItemResponse, 0, len(items))
	var nextCursor *string
	for _, item := range items {
		response, err := toTimelineItemResponse(item)
		if err != nil {
			return TimelineResponse{}, err
		}
		cursor := response.Cursor
		nextCursor = &cursor
		responses = append(responses, response)
	}
	return TimelineResponse{
		Scope:      toScopeResponse(scope),
		Items:      responses,
		NextCursor: nextCursor,
		SnapshotAt: snapshotAt.UTC(),
	}, nil
}

func toTimelineItemResponse(item TimelineItem) (TimelineItemResponse, error) {
	cursor, err := item.Cursor()
	if err != nil {
		return TimelineItemResponse{}, err
	}
	encoded, err := cursor.Encode()
	if err != nil {
		return TimelineItemResponse{}, err
	}
	return TimelineItemResponse{
		TimelineID:   item.TimelineID,
		Cursor:       encoded,
		OccurredAt:   item.OccurredAt.UTC(),
		SourceDomain: string(item.SourceDomain),
		SourceID:     item.SourceID,
		EventKind:    item.EventKind,
		RenderKind:   string(item.RenderKind),
		DisplayOnly:  item.DisplayOnly,
		ChannelID:    item.ChannelID,
		ProjectID:    item.ProjectID,
		TaskID:       item.TaskID,
		Actor: TimelineActorResponse{
			Type:            item.Actor.Type,
			Identity:        item.Actor.Identity,
			ProfileIdentity: item.Actor.ProfileIdentity,
			AgentInstanceID: item.Actor.AgentInstanceID,
		},
		Body:     item.Body,
		Summary:  item.Summary,
		Severity: item.Severity,
		Metadata: defaultJSON(item.Metadata),
		SourceRef: TimelineSourceResponse{
			Domain: item.SourceRef.Domain,
			Table:  item.SourceRef.Table,
			ID:     item.SourceRef.ID,
		},
	}, nil
}

func toScopeResponse(scope TimelineScope) TimelineScopeResponse {
	return TimelineScopeResponse{
		Kind:      string(scope.Kind),
		ChannelID: scope.ChannelID,
		ProjectID: scope.ProjectID,
	}
}

func defaultJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func parseRequiredInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, ErrInvalidScope
	}
	return value, nil
}
