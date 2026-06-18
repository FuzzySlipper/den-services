package delivery

import (
	"time"

	"den-services/shared/identity"
)

type IntentState string

const (
	IntentStatePending     IntentState = "pending"
	IntentStateClaimed     IntentState = "claimed"
	IntentStateRunning     IntentState = "running"
	IntentStateCompleted   IntentState = "completed"
	IntentStateFailed      IntentState = "failed"
	IntentStateExpired     IntentState = "expired"
	IntentStateCancelled   IntentState = "cancelled"
	IntentStateDisplayOnly IntentState = "display_only"
)

func (s IntentState) IsValid() bool {
	switch s {
	case IntentStatePending, IntentStateClaimed, IntentStateRunning, IntentStateCompleted,
		IntentStateFailed, IntentStateExpired, IntentStateCancelled, IntentStateDisplayOnly:
		return true
	}
	return false
}

func (s IntentState) IsTerminal() bool {
	switch s {
	case IntentStateCompleted, IntentStateFailed, IntentStateExpired, IntentStateCancelled, IntentStateDisplayOnly:
		return true
	}
	return false
}

type DeliveryIntent struct {
	id               int64
	targetIdentity   identity.AgentIdentity
	state            IntentState
	idempotencyKey   string
	createdAt        time.Time
	expiresAt        time.Time
	claimedAt        *time.Time
	claimToken       *string
	claimedBy        *identity.AgentIdentity
	completedAt      *time.Time
	sourceRef        *string
	channelMessageID *int64
	cutoverWatermark *string
}

func NewDeliveryIntent(target identity.AgentIdentity, idempotencyKey string, ttl time.Duration, sourceRef *string, channelMessageID *int64, now time.Time) (*DeliveryIntent, error) {
	intent := &DeliveryIntent{
		targetIdentity:   target,
		state:            IntentStatePending,
		idempotencyKey:   idempotencyKey,
		createdAt:        now.UTC(),
		expiresAt:        now.UTC().Add(ttl),
		sourceRef:        sourceRef,
		channelMessageID: channelMessageID,
	}
	if !intent.IsValid() {
		return nil, ErrInvalidIntent
	}
	return intent, nil
}

func rehydrateDeliveryIntent(id int64, target identity.AgentIdentity, state IntentState, idempotencyKey string, createdAt time.Time, expiresAt time.Time, claimedAt *time.Time, claimToken *string, claimedBy *identity.AgentIdentity, completedAt *time.Time, sourceRef *string, channelMessageID *int64, cutoverWatermark *string) (*DeliveryIntent, error) {
	intent := &DeliveryIntent{
		id:               id,
		targetIdentity:   target,
		state:            state,
		idempotencyKey:   idempotencyKey,
		createdAt:        createdAt,
		expiresAt:        expiresAt,
		claimedAt:        claimedAt,
		claimToken:       claimToken,
		claimedBy:        claimedBy,
		completedAt:      completedAt,
		sourceRef:        sourceRef,
		channelMessageID: channelMessageID,
		cutoverWatermark: cutoverWatermark,
	}
	if !intent.IsValid() {
		return nil, ErrInvalidIntent
	}
	return intent, nil
}

func (i *DeliveryIntent) IsValid() bool {
	return i.targetIdentity.IsValid() &&
		i.state.IsValid() &&
		i.idempotencyKey != "" &&
		!i.createdAt.IsZero() &&
		i.expiresAt.After(i.createdAt)
}

func (i *DeliveryIntent) ID() int64 {
	return i.id
}

func (i *DeliveryIntent) TargetIdentity() identity.AgentIdentity {
	return i.targetIdentity
}

func (i *DeliveryIntent) State() IntentState {
	return i.state
}

func (i *DeliveryIntent) IdempotencyKey() string {
	return i.idempotencyKey
}

func (i *DeliveryIntent) CreatedAt() time.Time {
	return i.createdAt
}

func (i *DeliveryIntent) ExpiresAt() time.Time {
	return i.expiresAt
}

func (i *DeliveryIntent) ClaimedAt() *time.Time {
	return i.claimedAt
}

func (i *DeliveryIntent) ClaimToken() *string {
	return i.claimToken
}

func (i *DeliveryIntent) ClaimedBy() *identity.AgentIdentity {
	return i.claimedBy
}

func (i *DeliveryIntent) CompletedAt() *time.Time {
	return i.completedAt
}

func (i *DeliveryIntent) SourceRef() *string {
	return i.sourceRef
}

func (i *DeliveryIntent) ChannelMessageID() *int64 {
	return i.channelMessageID
}

func (i *DeliveryIntent) CutoverWatermark() *string {
	return i.cutoverWatermark
}

type DeliveryEvent struct {
	id        int64
	intentID  int64
	eventType string
	payload   []byte
	createdAt time.Time
}
