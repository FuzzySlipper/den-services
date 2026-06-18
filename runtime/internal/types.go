package runtime

import (
	"time"

	"den-services/shared/identity"
)

type RuntimeState string

const (
	RuntimeStateStarting RuntimeState = "starting"
	RuntimeStateActive   RuntimeState = "active"
	RuntimeStateIdle     RuntimeState = "idle"
	RuntimeStateBusy     RuntimeState = "busy"
	RuntimeStateDegraded RuntimeState = "degraded"
	RuntimeStateStopped  RuntimeState = "stopped"
	RuntimeStateStale    RuntimeState = "stale"
	RuntimeStateDead     RuntimeState = "dead"
)

func (s RuntimeState) IsValid() bool {
	switch s {
	case RuntimeStateStarting, RuntimeStateActive, RuntimeStateIdle, RuntimeStateBusy,
		RuntimeStateDegraded, RuntimeStateStopped, RuntimeStateStale, RuntimeStateDead:
		return true
	}
	return false
}

type RuntimeInstance struct {
	instanceID      identity.AgentInstanceID
	profileIdentity identity.ProfileIdentity
	host            string
	pid             *int
	state           RuntimeState
	startedAt       time.Time
	lastHeartbeatAt *time.Time
	stoppedAt       *time.Time
	degradedReason  *string
}

func NewRuntimeInstance(
	instanceID identity.AgentInstanceID,
	profileIdentity identity.ProfileIdentity,
	host string,
	pid *int,
	now time.Time,
) (*RuntimeInstance, error) {
	instance := &RuntimeInstance{
		instanceID:      instanceID,
		profileIdentity: profileIdentity,
		host:            host,
		pid:             pid,
		state:           RuntimeStateStarting,
		startedAt:       now.UTC(),
	}
	if !instance.IsValid() {
		return nil, ErrInvalidRuntimeInstance
	}
	return instance, nil
}

func rehydrateRuntimeInstance(
	instanceID identity.AgentInstanceID,
	profileIdentity identity.ProfileIdentity,
	host string,
	pid *int,
	state RuntimeState,
	startedAt time.Time,
	lastHeartbeatAt *time.Time,
	stoppedAt *time.Time,
	degradedReason *string,
) (*RuntimeInstance, error) {
	instance := &RuntimeInstance{
		instanceID:      instanceID,
		profileIdentity: profileIdentity,
		host:            host,
		pid:             pid,
		state:           state,
		startedAt:       startedAt,
		lastHeartbeatAt: lastHeartbeatAt,
		stoppedAt:       stoppedAt,
		degradedReason:  degradedReason,
	}
	if !instance.IsValid() {
		return nil, ErrInvalidRuntimeInstance
	}
	return instance, nil
}

func (i *RuntimeInstance) IsValid() bool {
	return i.instanceID.IsValid() &&
		i.profileIdentity.IsValid() &&
		i.host != "" &&
		i.state.IsValid() &&
		!i.startedAt.IsZero()
}

func (i *RuntimeInstance) InstanceID() identity.AgentInstanceID {
	return i.instanceID
}

func (i *RuntimeInstance) ProfileIdentity() identity.ProfileIdentity {
	return i.profileIdentity
}

func (i *RuntimeInstance) Host() string {
	return i.host
}

func (i *RuntimeInstance) PID() *int {
	return i.pid
}

func (i *RuntimeInstance) State() RuntimeState {
	return i.state
}

func (i *RuntimeInstance) StartedAt() time.Time {
	return i.startedAt
}

func (i *RuntimeInstance) LastHeartbeatAt() *time.Time {
	return i.lastHeartbeatAt
}

func (i *RuntimeInstance) StoppedAt() *time.Time {
	return i.stoppedAt
}

func (i *RuntimeInstance) DegradedReason() *string {
	return i.degradedReason
}

type ChannelSubscription struct {
	subscriptionID     int64
	runtimeInstanceID  identity.AgentInstanceID
	channelID          int64
	cursorPosition     int64
	lastPolledAt       *time.Time
	wakePolicyOverride *string
	createdAt          time.Time
}

func NewChannelSubscription(instanceID identity.AgentInstanceID, channelID int64, wakePolicyOverride *string, now time.Time) (*ChannelSubscription, error) {
	subscription := &ChannelSubscription{
		runtimeInstanceID:  instanceID,
		channelID:          channelID,
		wakePolicyOverride: wakePolicyOverride,
		createdAt:          now.UTC(),
	}
	if !subscription.IsValid() {
		return nil, ErrInvalidSubscription
	}
	return subscription, nil
}

func rehydrateChannelSubscription(subscriptionID int64, instanceID identity.AgentInstanceID, channelID int64, cursorPosition int64, lastPolledAt *time.Time, wakePolicyOverride *string, createdAt time.Time) (*ChannelSubscription, error) {
	subscription := &ChannelSubscription{
		subscriptionID:     subscriptionID,
		runtimeInstanceID:  instanceID,
		channelID:          channelID,
		cursorPosition:     cursorPosition,
		lastPolledAt:       lastPolledAt,
		wakePolicyOverride: wakePolicyOverride,
		createdAt:          createdAt,
	}
	if !subscription.IsValid() {
		return nil, ErrInvalidSubscription
	}
	return subscription, nil
}

func (s *ChannelSubscription) IsValid() bool {
	return s.runtimeInstanceID.IsValid() && s.channelID > 0 && !s.createdAt.IsZero()
}

func (s *ChannelSubscription) SubscriptionID() int64 {
	return s.subscriptionID
}

func (s *ChannelSubscription) RuntimeInstanceID() identity.AgentInstanceID {
	return s.runtimeInstanceID
}

func (s *ChannelSubscription) ChannelID() int64 {
	return s.channelID
}

func (s *ChannelSubscription) CursorPosition() int64 {
	return s.cursorPosition
}

func (s *ChannelSubscription) LastPolledAt() *time.Time {
	return s.lastPolledAt
}

func (s *ChannelSubscription) WakePolicyOverride() *string {
	return s.wakePolicyOverride
}

func (s *ChannelSubscription) CreatedAt() time.Time {
	return s.createdAt
}
