package runtime

import (
	"errors"
	"time"

	"den-services/shared/identity"
)

type RegisterInstanceRequest struct {
	InstanceID      identity.AgentInstanceID `json:"instance_id"`
	ProfileIdentity identity.ProfileIdentity `json:"profile_identity"`
	Host            string                   `json:"host"`
	PID             *int                     `json:"pid,omitempty"`
}

func (r RegisterInstanceRequest) Validate() error {
	if !r.InstanceID.IsValid() || !r.ProfileIdentity.IsValid() || r.Host == "" {
		return ErrInvalidRuntimeInstance
	}
	return nil
}

type HeartbeatRequest struct {
	State *RuntimeState `json:"state,omitempty"`
}

func (r HeartbeatRequest) Validate() error {
	if r.State != nil && !r.State.IsValid() {
		return ErrInvalidRuntimeState
	}
	return nil
}

type RuntimeInstanceResponse struct {
	InstanceID      identity.AgentInstanceID `json:"instance_id"`
	ProfileIdentity identity.ProfileIdentity `json:"profile_identity"`
	Host            string                   `json:"host"`
	PID             *int                     `json:"pid,omitempty"`
	State           RuntimeState             `json:"state"`
	StartedAt       time.Time                `json:"started_at"`
	LastHeartbeatAt *time.Time               `json:"last_heartbeat_at,omitempty"`
	StoppedAt       *time.Time               `json:"stopped_at,omitempty"`
	DegradedReason  *string                  `json:"degraded_reason,omitempty"`
}

func toRuntimeInstanceResponse(instance *RuntimeInstance) RuntimeInstanceResponse {
	return RuntimeInstanceResponse{
		InstanceID:      instance.InstanceID(),
		ProfileIdentity: instance.ProfileIdentity(),
		Host:            instance.Host(),
		PID:             instance.PID(),
		State:           instance.State(),
		StartedAt:       instance.StartedAt(),
		LastHeartbeatAt: instance.LastHeartbeatAt(),
		StoppedAt:       instance.StoppedAt(),
		DegradedReason:  instance.DegradedReason(),
	}
}

type CreateSubscriptionRequest struct {
	RuntimeInstanceID  identity.AgentInstanceID `json:"runtime_instance_id"`
	ChannelID          int64                    `json:"channel_id"`
	WakePolicyOverride *string                  `json:"wake_policy_override,omitempty"`
}

func (r CreateSubscriptionRequest) Validate() error {
	if !r.RuntimeInstanceID.IsValid() || r.ChannelID <= 0 {
		return ErrInvalidSubscription
	}
	return nil
}

type ChannelSubscriptionResponse struct {
	SubscriptionID     int64                    `json:"subscription_id"`
	RuntimeInstanceID  identity.AgentInstanceID `json:"runtime_instance_id"`
	ChannelID          int64                    `json:"channel_id"`
	CursorPosition     int64                    `json:"cursor_position"`
	LastPolledAt       *time.Time               `json:"last_polled_at,omitempty"`
	WakePolicyOverride *string                  `json:"wake_policy_override,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
}

func toSubscriptionResponse(subscription *ChannelSubscription) ChannelSubscriptionResponse {
	return ChannelSubscriptionResponse{
		SubscriptionID:     subscription.SubscriptionID(),
		RuntimeInstanceID:  subscription.RuntimeInstanceID(),
		ChannelID:          subscription.ChannelID(),
		CursorPosition:     subscription.CursorPosition(),
		LastPolledAt:       subscription.LastPolledAt(),
		WakePolicyOverride: subscription.WakePolicyOverride(),
		CreatedAt:          subscription.CreatedAt(),
	}
}

type StreamResponse struct {
	SubscriptionID int64         `json:"subscription_id"`
	After          int64         `json:"after"`
	CursorPosition int64         `json:"cursor_position"`
	Events         []StreamEvent `json:"events"`
}

type StreamEvent struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

func parseRequiredInt64(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("value is required")
	}
	var parsed int64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, errors.New("value must be numeric")
		}
		parsed = parsed*10 + int64(ch-'0')
	}
	return parsed, nil
}
