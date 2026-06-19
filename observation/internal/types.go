package observation

import (
	"encoding/json"
	"time"

	"den-services/shared/identity"
)

type SourceDomain string

const (
	SourceDomainObservation  SourceDomain = "observation"
	SourceDomainDelivery     SourceDomain = "delivery"
	SourceDomainRuntime      SourceDomain = "runtime"
	SourceDomainConversation SourceDomain = "conversation"
	SourceDomainLegacy       SourceDomain = "legacy"
)

func (d SourceDomain) IsValid() bool {
	switch d {
	case SourceDomainObservation, SourceDomainDelivery, SourceDomainRuntime, SourceDomainConversation, SourceDomainLegacy:
		return true
	}
	return false
}

type ActivityEvent struct {
	eventID           int64
	sourceDomain      SourceDomain
	eventType         string
	agentIdentity     *identity.AgentIdentity
	runtimeInstanceID *identity.AgentInstanceID
	payload           json.RawMessage
	displayOnly       bool
	createdAt         time.Time
}

func NewActivityEvent(
	sourceDomain SourceDomain,
	eventType string,
	agentIdentity *identity.AgentIdentity,
	runtimeInstanceID *identity.AgentInstanceID,
	payload json.RawMessage,
	now time.Time,
) (*ActivityEvent, error) {
	event := &ActivityEvent{
		sourceDomain:      sourceDomain,
		eventType:         eventType,
		agentIdentity:     agentIdentity,
		runtimeInstanceID: runtimeInstanceID,
		payload:           normalizePayload(payload),
		displayOnly:       true,
		createdAt:         now.UTC(),
	}
	if !event.IsValid() {
		return nil, ErrInvalidActivityEvent
	}
	return event, nil
}

func rehydrateActivityEvent(
	eventID int64,
	sourceDomain SourceDomain,
	eventType string,
	agentIdentity *identity.AgentIdentity,
	runtimeInstanceID *identity.AgentInstanceID,
	payload json.RawMessage,
	displayOnly bool,
	createdAt time.Time,
) (*ActivityEvent, error) {
	event := &ActivityEvent{
		eventID:           eventID,
		sourceDomain:      sourceDomain,
		eventType:         eventType,
		agentIdentity:     agentIdentity,
		runtimeInstanceID: runtimeInstanceID,
		payload:           normalizePayload(payload),
		displayOnly:       displayOnly,
		createdAt:         createdAt.UTC(),
	}
	if !event.IsValid() {
		return nil, ErrInvalidActivityEvent
	}
	return event, nil
}

func (e *ActivityEvent) IsValid() bool {
	if !e.sourceDomain.IsValid() || e.eventType == "" || e.createdAt.IsZero() {
		return false
	}
	if e.agentIdentity != nil && !e.agentIdentity.IsValid() {
		return false
	}
	if e.runtimeInstanceID != nil && !e.runtimeInstanceID.IsValid() {
		return false
	}
	return json.Valid(e.payload)
}

func (e *ActivityEvent) EventID() int64 {
	return e.eventID
}

func (e *ActivityEvent) SourceDomain() SourceDomain {
	return e.sourceDomain
}

func (e *ActivityEvent) EventType() string {
	return e.eventType
}

func (e *ActivityEvent) AgentIdentity() *identity.AgentIdentity {
	return e.agentIdentity
}

func (e *ActivityEvent) RuntimeInstanceID() *identity.AgentInstanceID {
	return e.runtimeInstanceID
}

func (e *ActivityEvent) Payload() json.RawMessage {
	return append(json.RawMessage(nil), e.payload...)
}

func (e *ActivityEvent) DisplayOnly() bool {
	return e.displayOnly
}

func (e *ActivityEvent) CreatedAt() time.Time {
	return e.createdAt
}

func normalizePayload(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), payload...)
}

type LaneEvent struct {
	EventID           string
	SourceDomain      SourceDomain
	EventType         string
	AgentIdentity     *identity.AgentIdentity
	RuntimeInstanceID *identity.AgentInstanceID
	Payload           json.RawMessage
	DisplayOnly       bool
	CreatedAt         time.Time
}

type ActiveWorkItem struct {
	IntentID          int64
	TargetIdentity    identity.AgentIdentity
	State             string
	ClaimedBy         *identity.AgentIdentity
	RuntimeInstanceID *identity.AgentInstanceID
	RuntimeState      *string
	SourceRef         *string
	ChannelMessageID  *int64
	CreatedAt         time.Time
}

type RuntimeProjection struct {
	RuntimeInstanceID identity.AgentInstanceID
	ProfileIdentity   identity.ProfileIdentity
	Host              string
	State             string
	LastHeartbeatAt   *time.Time
	StartedAt         time.Time
	DisplayOnly       bool
}

type AgentOverview struct {
	AgentID          string
	RuntimeInstances []RuntimeProjection
	ActiveWork       []ActiveWorkItem
}
