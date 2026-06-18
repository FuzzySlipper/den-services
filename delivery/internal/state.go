package delivery

import (
	"time"

	"den-services/shared/identity"
)

func (i *DeliveryIntent) applyClaim(token string, claimedBy identity.AgentIdentity, at time.Time) error {
	if i.state != IntentStatePending {
		return ErrIntentAlreadyClaimed
	}
	claimedAt := at.UTC()
	i.state = IntentStateClaimed
	i.claimToken = &token
	i.claimedBy = &claimedBy
	i.claimedAt = &claimedAt
	return nil
}

func (i *DeliveryIntent) canReport(eventType string, token string) error {
	if i.claimToken == nil || *i.claimToken != token {
		return ErrInvalidClaimToken
	}
	if i.state.IsTerminal() {
		return ErrIntentAlreadyCompleted
	}
	switch eventType {
	case "running":
		if i.state != IntentStateClaimed {
			return ErrInvalidLifecycleEvent
		}
	case "completed", "failed":
		if i.state != IntentStateRunning && i.state != IntentStateClaimed {
			return ErrInvalidLifecycleEvent
		}
	default:
		return ErrInvalidLifecycleEvent
	}
	return nil
}
