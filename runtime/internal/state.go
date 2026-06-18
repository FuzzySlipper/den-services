package runtime

import "time"

func (i *RuntimeInstance) applyHeartbeat(at time.Time) {
	heartbeatAt := at.UTC()
	i.lastHeartbeatAt = &heartbeatAt
	if i.state == RuntimeStateStarting || i.state == RuntimeStateStale || i.state == RuntimeStateDead {
		i.state = RuntimeStateActive
	}
}

func stateForHeartbeat(lastHeartbeatAt *time.Time, current RuntimeState, now time.Time, staleThreshold time.Duration, deadThreshold time.Duration) RuntimeState {
	if current == RuntimeStateStopped {
		return current
	}
	if lastHeartbeatAt == nil {
		return current
	}
	age := now.Sub(*lastHeartbeatAt)
	if age >= deadThreshold {
		return RuntimeStateDead
	}
	if age >= staleThreshold {
		return RuntimeStateStale
	}
	return current
}
