package observation

import (
	"encoding/json"
	"errors"
	"time"

	"den-services/shared/identity"
)

type CreateLifecycleEventRequest struct {
	SourceDomain      SourceDomain              `json:"source_domain"`
	EventType         string                    `json:"event_type"`
	AgentIdentity     *identity.AgentIdentity   `json:"agent_identity,omitempty"`
	RuntimeInstanceID *identity.AgentInstanceID `json:"runtime_instance_id,omitempty"`
	Payload           json.RawMessage           `json:"payload,omitempty"`
}

func (r CreateLifecycleEventRequest) Validate() error {
	if !r.SourceDomain.IsValid() {
		return ErrInvalidSourceDomain
	}
	if r.EventType == "" {
		return ErrInvalidActivityEvent
	}
	if r.AgentIdentity != nil && !r.AgentIdentity.IsValid() {
		return ErrInvalidActivityEvent
	}
	if r.RuntimeInstanceID != nil && !r.RuntimeInstanceID.IsValid() {
		return ErrInvalidActivityEvent
	}
	if len(r.Payload) > 0 && !json.Valid(r.Payload) {
		return ErrInvalidActivityEvent
	}
	return nil
}

type ActivityEventResponse struct {
	EventID           int64                     `json:"event_id"`
	SourceDomain      SourceDomain              `json:"source_domain"`
	EventType         string                    `json:"event_type"`
	AgentIdentity     *identity.AgentIdentity   `json:"agent_identity,omitempty"`
	RuntimeInstanceID *identity.AgentInstanceID `json:"runtime_instance_id,omitempty"`
	Payload           json.RawMessage           `json:"payload"`
	DisplayOnly       bool                      `json:"display_only"`
	CreatedAt         time.Time                 `json:"created_at"`
}

func toActivityEventResponse(event *ActivityEvent) ActivityEventResponse {
	return ActivityEventResponse{
		EventID:           event.EventID(),
		SourceDomain:      event.SourceDomain(),
		EventType:         event.EventType(),
		AgentIdentity:     event.AgentIdentity(),
		RuntimeInstanceID: event.RuntimeInstanceID(),
		Payload:           event.Payload(),
		DisplayOnly:       event.DisplayOnly(),
		CreatedAt:         event.CreatedAt(),
	}
}

type LaneResponse struct {
	Events []LaneEventResponse `json:"events"`
}

type LaneEventResponse struct {
	EventID           string                    `json:"event_id"`
	SourceDomain      SourceDomain              `json:"source_domain"`
	EventType         string                    `json:"event_type"`
	AgentIdentity     *identity.AgentIdentity   `json:"agent_identity,omitempty"`
	RuntimeInstanceID *identity.AgentInstanceID `json:"runtime_instance_id,omitempty"`
	Payload           json.RawMessage           `json:"payload"`
	DisplayOnly       bool                      `json:"display_only"`
	CreatedAt         time.Time                 `json:"created_at"`
}

func toLaneResponse(events []LaneEvent) LaneResponse {
	responses := make([]LaneEventResponse, 0, len(events))
	for _, event := range events {
		responses = append(responses, LaneEventResponse{
			EventID:           event.EventID,
			SourceDomain:      event.SourceDomain,
			EventType:         event.EventType,
			AgentIdentity:     event.AgentIdentity,
			RuntimeInstanceID: event.RuntimeInstanceID,
			Payload:           append(json.RawMessage(nil), event.Payload...),
			DisplayOnly:       event.DisplayOnly,
			CreatedAt:         event.CreatedAt,
		})
	}
	return LaneResponse{Events: responses}
}

type ActiveWorkResponse struct {
	Items []ActiveWorkItemResponse `json:"items"`
}

type ActiveWorkItemResponse struct {
	IntentID          int64                     `json:"intent_id"`
	TargetIdentity    identity.AgentIdentity    `json:"target_identity"`
	State             string                    `json:"state"`
	ClaimedBy         *identity.AgentIdentity   `json:"claimed_by,omitempty"`
	RuntimeInstanceID *identity.AgentInstanceID `json:"runtime_instance_id,omitempty"`
	RuntimeState      *string                   `json:"runtime_state,omitempty"`
	SourceRef         *string                   `json:"source_ref,omitempty"`
	ChannelMessageID  *int64                    `json:"channel_message_id,omitempty"`
	CreatedAt         time.Time                 `json:"created_at"`
}

func toActiveWorkResponse(items []ActiveWorkItem) ActiveWorkResponse {
	responses := make([]ActiveWorkItemResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, ActiveWorkItemResponse{
			IntentID:          item.IntentID,
			TargetIdentity:    item.TargetIdentity,
			State:             item.State,
			ClaimedBy:         item.ClaimedBy,
			RuntimeInstanceID: item.RuntimeInstanceID,
			RuntimeState:      item.RuntimeState,
			SourceRef:         item.SourceRef,
			ChannelMessageID:  item.ChannelMessageID,
			CreatedAt:         item.CreatedAt,
		})
	}
	return ActiveWorkResponse{Items: responses}
}

type AgentOverviewResponse struct {
	AgentID          string                      `json:"agent_id"`
	RuntimeInstances []RuntimeProjectionResponse `json:"runtime_instances"`
	ActiveWork       []ActiveWorkItemResponse    `json:"active_work"`
}

type RuntimeProjectionResponse struct {
	RuntimeInstanceID identity.AgentInstanceID `json:"runtime_instance_id"`
	ProfileIdentity   identity.ProfileIdentity `json:"profile_identity"`
	Host              string                   `json:"host"`
	State             string                   `json:"state"`
	LastHeartbeatAt   *time.Time               `json:"last_heartbeat_at,omitempty"`
	StartedAt         time.Time                `json:"started_at"`
	DisplayOnly       bool                     `json:"display_only"`
}

func toAgentOverviewResponse(overview AgentOverview) AgentOverviewResponse {
	runtimes := make([]RuntimeProjectionResponse, 0, len(overview.RuntimeInstances))
	for _, runtime := range overview.RuntimeInstances {
		runtimes = append(runtimes, RuntimeProjectionResponse{
			RuntimeInstanceID: runtime.RuntimeInstanceID,
			ProfileIdentity:   runtime.ProfileIdentity,
			Host:              runtime.Host,
			State:             runtime.State,
			LastHeartbeatAt:   runtime.LastHeartbeatAt,
			StartedAt:         runtime.StartedAt,
			DisplayOnly:       runtime.DisplayOnly,
		})
	}
	return AgentOverviewResponse{
		AgentID:          overview.AgentID,
		RuntimeInstances: runtimes,
		ActiveWork:       toActiveWorkResponse(overview.ActiveWork).Items,
	}
}

func parseRequiredInt(value string) (int, error) {
	if value == "" {
		return 0, errors.New("value is required")
	}
	parsed := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, errors.New("value must be numeric")
		}
		parsed = parsed*10 + int(ch-'0')
	}
	return parsed, nil
}
