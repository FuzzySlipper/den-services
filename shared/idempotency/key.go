package idempotency

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"den-services/shared/identity"
)

const keyPartCount = 4

type Key struct {
	operation     string
	channelID     string
	targetProfile identity.ProfileIdentity
	nonce         string
}

func NewKey(operation string, channelID string, targetProfile identity.ProfileIdentity, nonce string) (Key, error) {
	key := Key{
		operation:     operation,
		channelID:     channelID,
		targetProfile: targetProfile,
		nonce:         nonce,
	}
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return key, nil
}

func Generate(operation string, channelID string, targetProfile identity.ProfileIdentity) (Key, error) {
	nonce, err := generateNonce()
	if err != nil {
		return Key{}, err
	}
	return NewKey(operation, channelID, targetProfile, nonce)
}

func Parse(raw string) (Key, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != keyPartCount {
		return Key{}, fmt.Errorf("%w: expected %d parts", ErrInvalidKey, keyPartCount)
	}
	return NewKey(parts[0], parts[1], identity.ProfileIdentity(parts[2]), parts[3])
}

func (k Key) Validate() error {
	if !validPart(k.operation) {
		return fmt.Errorf("%w: operation is required", ErrInvalidKey)
	}
	if !validPart(k.channelID) {
		return fmt.Errorf("%w: channel_id is required", ErrInvalidKey)
	}
	if !k.targetProfile.IsValid() || strings.Contains(k.targetProfile.String(), ":") {
		return fmt.Errorf("%w: target_profile is invalid", ErrInvalidKey)
	}
	if !validPart(k.nonce) {
		return fmt.Errorf("%w: nonce is required", ErrInvalidKey)
	}
	return nil
}

func (k Key) String() string {
	return strings.Join([]string{k.operation, k.channelID, k.targetProfile.String(), k.nonce}, ":")
}

func (k Key) Operation() string {
	return k.operation
}

func (k Key) ChannelID() string {
	return k.channelID
}

func (k Key) TargetProfile() identity.ProfileIdentity {
	return k.targetProfile
}

func (k Key) Nonce() string {
	return k.nonce
}

func validPart(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.Contains(value, ":")
}

func generateNonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

var (
	ErrInvalidKey = errors.New("invalid idempotency key") //nolint:gochecknoglobals
)
