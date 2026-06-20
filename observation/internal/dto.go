package observation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	if strings.TrimSpace(r.EventType) == "" {
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
	if err := validateAgentActivityPayload(r.EventType, r.Payload); err != nil {
		return err
	}
	return nil
}

type agentActivityPayload struct {
	Kind          string                  `json:"kind"`
	SchemaVersion *int                    `json:"schema_version"`
	Summary       string                  `json:"summary"`
	Severity      AgentActivitySeverity   `json:"severity"`
	Visibility    AgentActivityVisibility `json:"visibility"`
	Adapter       AgentActivityAdapter    `json:"adapter"`
	Surface       AgentActivitySurface    `json:"surface"`
	WorkRef       *AgentActivityWorkRef   `json:"work_ref,omitempty"`
	SessionKey    string                  `json:"session_key,omitempty"`
	ToolName      string                  `json:"tool_name,omitempty"`
	Model         string                  `json:"model,omitempty"`
	ReasonCode    string                  `json:"reason_code,omitempty"`
	ResultRef     *AgentActivityResultRef `json:"result_ref,omitempty"`
}

type AgentActivitySeverity string

const (
	AgentActivitySeverityInfo    AgentActivitySeverity = "info"
	AgentActivitySeveritySuccess AgentActivitySeverity = "success"
	AgentActivitySeverityWarning AgentActivitySeverity = "warning"
	AgentActivitySeverityError   AgentActivitySeverity = "error"
)

func (s AgentActivitySeverity) IsValid() bool {
	switch s {
	case AgentActivitySeverityInfo, AgentActivitySeveritySuccess, AgentActivitySeverityWarning, AgentActivitySeverityError:
		return true
	}
	return false
}

type AgentActivityVisibility string

const (
	AgentActivityVisibilityChannel AgentActivityVisibility = "channel"
	AgentActivityVisibilityTask    AgentActivityVisibility = "task"
	AgentActivityVisibilityAgent   AgentActivityVisibility = "agent"
	AgentActivityVisibilityDebug   AgentActivityVisibility = "debug"
)

func (v AgentActivityVisibility) IsValid() bool {
	switch v {
	case AgentActivityVisibilityChannel, AgentActivityVisibilityTask, AgentActivityVisibilityAgent, AgentActivityVisibilityDebug:
		return true
	}
	return false
}

type AgentActivityAdapter string

const (
	AgentActivityAdapterHermes      AgentActivityAdapter = "hermes"
	AgentActivityAdapterPiCrew      AgentActivityAdapter = "pi-crew"
	AgentActivityAdapterDenServices AgentActivityAdapter = "den-services"
	AgentActivityAdapterDenChannels AgentActivityAdapter = "den-channels"
	AgentActivityAdapterDenWeb      AgentActivityAdapter = "den-web"
)

func (a AgentActivityAdapter) IsValid() bool {
	switch a {
	case AgentActivityAdapterHermes, AgentActivityAdapterPiCrew, AgentActivityAdapterDenServices, AgentActivityAdapterDenChannels, AgentActivityAdapterDenWeb:
		return true
	}
	return false
}

type AgentActivitySurface string

const (
	AgentActivitySurfaceChannel     AgentActivitySurface = "channel"
	AgentActivitySurfaceTask        AgentActivitySurface = "task"
	AgentActivitySurfaceWorker      AgentActivitySurface = "worker"
	AgentActivitySurfaceReview      AgentActivitySurface = "review"
	AgentActivitySurfaceDirectDebug AgentActivitySurface = "direct-debug"
	AgentActivitySurfaceGateway     AgentActivitySurface = "gateway"
	AgentActivitySurfaceRuntime     AgentActivitySurface = "runtime"
	AgentActivitySurfaceObservation AgentActivitySurface = "observation"
)

func (s AgentActivitySurface) IsValid() bool {
	switch s {
	case AgentActivitySurfaceChannel, AgentActivitySurfaceTask, AgentActivitySurfaceWorker, AgentActivitySurfaceReview,
		AgentActivitySurfaceDirectDebug, AgentActivitySurfaceGateway, AgentActivitySurfaceRuntime, AgentActivitySurfaceObservation:
		return true
	}
	return false
}

type AgentActivityWorkRef struct {
	ProjectID        string `json:"project_id,omitempty"`
	TaskID           *int64 `json:"task_id,omitempty"`
	AssignmentID     string `json:"assignment_id,omitempty"`
	RunID            string `json:"run_id,omitempty"`
	ReviewRoundID    string `json:"review_round_id,omitempty"`
	ChannelID        *int64 `json:"channel_id,omitempty"`
	ChannelMessageID *int64 `json:"channel_message_id,omitempty"`
}

func (r AgentActivityWorkRef) HasReference() bool {
	return strings.TrimSpace(r.ProjectID) != "" ||
		r.TaskID != nil ||
		strings.TrimSpace(r.AssignmentID) != "" ||
		strings.TrimSpace(r.RunID) != "" ||
		strings.TrimSpace(r.ReviewRoundID) != "" ||
		r.ChannelID != nil ||
		r.ChannelMessageID != nil
}

type AgentActivityResultRef struct {
	DocumentSlug string `json:"document_slug,omitempty"`
	MessageID    *int64 `json:"message_id,omitempty"`
	Commit       string `json:"commit,omitempty"`
	ArtifactPath string `json:"artifact_path,omitempty"`
}

func (r AgentActivityResultRef) HasReference() bool {
	return strings.TrimSpace(r.DocumentSlug) != "" ||
		r.MessageID != nil ||
		strings.TrimSpace(r.Commit) != "" ||
		strings.TrimSpace(r.ArtifactPath) != ""
}

func validateAgentActivityPayload(eventType string, payloadJSON json.RawMessage) error {
	if len(payloadJSON) == 0 {
		return fmt.Errorf("%w: payload is required", ErrInvalidActivityEvent)
	}
	var payload agentActivityPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return fmt.Errorf("%w: decoding payload: %w", ErrInvalidActivityEvent, err)
	}
	if payload.Kind != "agent_activity.v1" {
		return fmt.Errorf("%w: payload.kind must be agent_activity.v1", ErrInvalidActivityEvent)
	}
	if payload.SchemaVersion == nil || *payload.SchemaVersion != 1 {
		return fmt.Errorf("%w: payload.schema_version must be 1", ErrInvalidActivityEvent)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return fmt.Errorf("%w: payload.summary is required", ErrInvalidActivityEvent)
	}
	if len(payload.Summary) > 240 {
		return fmt.Errorf("%w: payload.summary must be 240 characters or fewer", ErrInvalidActivityEvent)
	}
	if !payload.Severity.IsValid() {
		return fmt.Errorf("%w: payload.severity is invalid", ErrInvalidActivityEvent)
	}
	if !payload.Visibility.IsValid() {
		return fmt.Errorf("%w: payload.visibility is invalid", ErrInvalidActivityEvent)
	}
	if !payload.Adapter.IsValid() {
		return fmt.Errorf("%w: payload.adapter is invalid", ErrInvalidActivityEvent)
	}
	if !payload.Surface.IsValid() {
		return fmt.Errorf("%w: payload.surface is invalid", ErrInvalidActivityEvent)
	}
	return validateAgentActivityEventSpecificFields(eventType, payload)
}

func validateAgentActivityEventSpecificFields(eventType string, payload agentActivityPayload) error {
	switch eventType {
	case "agent_session_started", "agent_session_resumed":
		return requirePayloadFields(payload, requiredPayloadFieldSessionKey)
	case "agent_session_idle", "agent_session_stopped":
		return requirePayloadFields(payload, requiredPayloadFieldSessionKey)
	case "agent_session_blocked", "agent_session_failed":
		return requirePayloadFields(payload, requiredPayloadFieldSessionKey, requiredPayloadFieldReasonCode)
	case "work_started", "work_checkpoint":
		return requirePayloadFields(payload, requiredPayloadFieldWorkRef)
	case "work_waiting", "work_failed":
		return requirePayloadFields(payload, requiredPayloadFieldWorkRef, requiredPayloadFieldReasonCode)
	case "work_completed":
		return requirePayloadFields(payload, requiredPayloadFieldWorkRef, requiredPayloadFieldResultRef)
	case "model_turn_started", "model_turn_completed":
		return requirePayloadFields(payload, requiredPayloadFieldSessionKey)
	case "tool_call_started":
		return requirePayloadFields(payload, requiredPayloadFieldToolName)
	case "tool_call_completed":
		return requirePayloadFields(payload, requiredPayloadFieldToolName, requiredPayloadFieldResultRef)
	case "tool_call_failed":
		return requirePayloadFields(payload, requiredPayloadFieldToolName, requiredPayloadFieldReasonCode)
	case "adapter_connected", "adapter_recovered":
		return nil
	case "adapter_disconnected", "adapter_degraded":
		return requirePayloadFields(payload, requiredPayloadFieldReasonCode)
	default:
		return nil
	}
}

type requiredPayloadField string

const (
	requiredPayloadFieldSessionKey requiredPayloadField = "session_key"
	requiredPayloadFieldWorkRef    requiredPayloadField = "work_ref"
	requiredPayloadFieldToolName   requiredPayloadField = "tool_name"
	requiredPayloadFieldReasonCode requiredPayloadField = "reason_code"
	requiredPayloadFieldResultRef  requiredPayloadField = "result_ref"
)

func requirePayloadFields(payload agentActivityPayload, fields ...requiredPayloadField) error {
	for _, field := range fields {
		if payloadFieldMissing(payload, field) {
			return fmt.Errorf("%w: payload.%s is required for %s", ErrInvalidActivityEvent, field, payload.Kind)
		}
	}
	return nil
}

func payloadFieldMissing(payload agentActivityPayload, field requiredPayloadField) bool {
	switch field {
	case requiredPayloadFieldSessionKey:
		return strings.TrimSpace(payload.SessionKey) == ""
	case requiredPayloadFieldWorkRef:
		return payload.WorkRef == nil || !payload.WorkRef.HasReference()
	case requiredPayloadFieldToolName:
		return strings.TrimSpace(payload.ToolName) == ""
	case requiredPayloadFieldReasonCode:
		return strings.TrimSpace(payload.ReasonCode) == ""
	case requiredPayloadFieldResultRef:
		return payload.ResultRef == nil || !payload.ResultRef.HasReference()
	default:
		return true
	}
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
	ActivityEvents   []LaneEventResponse         `json:"activity_events"`
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
		ActivityEvents:   toLaneResponse(overview.ActivityEvents).Events,
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
