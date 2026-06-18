package idempotency

import (
	"errors"
	"testing"

	"den-services/shared/identity"
)

func TestNewKey(t *testing.T) {
	key, err := NewKey("direct-agent-message", "channel-1", identity.ProfileIdentity("planner"), "nonce-1")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	want := "direct-agent-message:channel-1:planner:nonce-1"
	if key.String() != want {
		t.Fatalf("String() = %q, want %q", key.String(), want)
	}
}

func TestParse(t *testing.T) {
	key, err := Parse("op:channel:profile:nonce")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if key.Operation() != "op" || key.ChannelID() != "channel" || key.TargetProfile() != "profile" || key.Nonce() != "nonce" {
		t.Fatalf("Parse() = %#v", key)
	}
}

func TestParseRejectsInvalidKeys(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"too few parts", "op:channel:profile"},
		{"empty operation", ":channel:profile:nonce"},
		{"empty profile", "op:channel::nonce"},
		{"too many parts", "op:channel:profile:nonce:extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.raw)
			if !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Parse() error = %v, want %v", err, ErrInvalidKey)
			}
		})
	}
}

func TestGenerateUsesValidNonce(t *testing.T) {
	key, err := Generate("op", "channel", "profile")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("generated key Validate() error = %v", err)
	}
	if key.Nonce() == "" {
		t.Fatal("generated key nonce is empty")
	}
}
