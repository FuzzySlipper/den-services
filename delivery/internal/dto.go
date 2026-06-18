package delivery

import (
	"encoding/json"
	"errors"
	"time"

	"den-services/shared/idempotency"
	"den-services/shared/identity"
)

type CreateIntentRequest struct {
	TargetIdentity   identity.AgentIdentity `json:"target_identity"`
	IdempotencyKey   string                 `json:"idempotency_key"`
	TTLSeconds       *int64                 `json:"ttl_seconds,omitempty"`
	SourceRef        *string                `json:"source_ref,omitempty"`
	ChannelMessageID *int64                 `json:"channel_message_id,omitempty"`
}

func (r CreateIntentRequest) Validate() error {
	if !r.TargetIdentity.IsValid() {
		return ErrInvalidIntent
	}
	key, err := idempotency.Parse(r.IdempotencyKey)
	if err != nil {
		return err
	}
	if key.TargetProfile() != r.TargetIdentity.Profile {
		return ErrInvalidIntent
	}
	if r.TTLSeconds != nil && *r.TTLSeconds <= 0 {
		return ErrInvalidIntent
	}
	return nil
}

type ClaimRequest struct {
	ClaimToken string                 `json:"claim_token"`
	ClaimedBy  identity.AgentIdentity `json:"claimed_by"`
}

func (r ClaimRequest) Validate() error {
	if r.ClaimToken == "" || !r.ClaimedBy.IsValid() {
		return ErrInvalidIntent
	}
	return nil
}

type LifecycleEventRequest struct {
	EventType  string          `json:"event_type"`
	ClaimToken string          `json:"claim_token"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func (r LifecycleEventRequest) Validate() error {
	if r.ClaimToken == "" {
		return ErrInvalidClaimToken
	}
	switch r.EventType {
	case "running", "completed", "failed":
		return nil
	default:
		return ErrInvalidLifecycleEvent
	}
}

type IntentResponse struct {
	ID               int64                   `json:"id"`
	TargetIdentity   identity.AgentIdentity  `json:"target_identity"`
	State            IntentState             `json:"state"`
	IdempotencyKey   string                  `json:"idempotency_key"`
	CreatedAt        time.Time               `json:"created_at"`
	ExpiresAt        time.Time               `json:"expires_at"`
	ClaimedAt        *time.Time              `json:"claimed_at,omitempty"`
	ClaimedBy        *identity.AgentIdentity `json:"claimed_by,omitempty"`
	CompletedAt      *time.Time              `json:"completed_at,omitempty"`
	SourceRef        *string                 `json:"source_ref,omitempty"`
	ChannelMessageID *int64                  `json:"channel_message_id,omitempty"`
	CutoverWatermark *string                 `json:"cutover_watermark,omitempty"`
}

func toIntentResponse(intent *DeliveryIntent) IntentResponse {
	return IntentResponse{
		ID:               intent.ID(),
		TargetIdentity:   intent.TargetIdentity(),
		State:            intent.State(),
		IdempotencyKey:   intent.IdempotencyKey(),
		CreatedAt:        intent.CreatedAt(),
		ExpiresAt:        intent.ExpiresAt(),
		ClaimedAt:        intent.ClaimedAt(),
		ClaimedBy:        intent.ClaimedBy(),
		CompletedAt:      intent.CompletedAt(),
		SourceRef:        intent.SourceRef(),
		ChannelMessageID: intent.ChannelMessageID(),
		CutoverWatermark: intent.CutoverWatermark(),
	}
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
