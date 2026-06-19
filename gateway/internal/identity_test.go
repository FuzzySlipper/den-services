package gateway

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestIdentityTranslationMapsKnownAgentIdentity(t *testing.T) {
	translation := mustTestTranslation(t)
	body := []byte(`{"member_identity":"pi-crew-planner","concrete_identity":"pi-crew-planner@den-srv","hermes_session_key":"sess-1","message":"hello"}`)

	translated, err := translation.TranslateJSON(body)
	if err != nil {
		t.Fatalf("TranslateJSON() error = %v", err)
	}

	var payload struct {
		TargetIdentity struct {
			Profile    string `json:"profile"`
			InstanceID string `json:"instance_id"`
			SessionKey string `json:"session_key"`
		} `json:"target_identity"`
		MemberIdentity string `json:"member_identity"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal(translated, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.TargetIdentity.Profile != "pi-crew-planner" {
		t.Fatalf("profile = %s", payload.TargetIdentity.Profile)
	}
	if payload.TargetIdentity.InstanceID != "pi-crew-planner@den-srv" {
		t.Fatalf("instance_id = %s", payload.TargetIdentity.InstanceID)
	}
	if payload.TargetIdentity.SessionKey != "sess-1" {
		t.Fatalf("session_key = %s", payload.TargetIdentity.SessionKey)
	}
	if payload.MemberIdentity != "" {
		t.Fatalf("legacy member_identity leaked as %q", payload.MemberIdentity)
	}
	if payload.Message != "hello" {
		t.Fatalf("message = %s", payload.Message)
	}
}

func TestIdentityTranslationUsesStaticMappingWhenInstanceIsExplicitlyConfigured(t *testing.T) {
	translation, err := newIdentityTranslation(identityTranslationFile{
		Enabled: true,
		Targets: []identityTargetFile{{
			CanonicalField: "target_identity",
			Required:       true,
		}},
		Mappings: []identityMappingFile{{
			LegacyIdentity: "den-mcp-runner",
			Profile:        "den-mcp-runner",
			InstanceID:     "den-mcp-runner@configured",
		}},
	})
	if err != nil {
		t.Fatalf("newIdentityTranslation() error = %v", err)
	}

	translated, err := translation.TranslateJSON([]byte(`{"agent_identity":"den-mcp-runner"}`))
	if err != nil {
		t.Fatalf("TranslateJSON() error = %v", err)
	}

	var payload struct {
		TargetIdentity struct {
			Profile    string `json:"profile"`
			InstanceID string `json:"instance_id"`
		} `json:"target_identity"`
	}
	if err := json.Unmarshal(translated, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.TargetIdentity.Profile != "den-mcp-runner" || payload.TargetIdentity.InstanceID != "den-mcp-runner@configured" {
		t.Fatalf("target_identity = %+v", payload.TargetIdentity)
	}
}

func TestIdentityTranslationRejectsUnknownLegacyIdentity(t *testing.T) {
	translation := mustTestTranslation(t)

	_, err := translation.TranslateJSON([]byte(`{"member_identity":"unknown","concrete_identity":"unknown@host"}`))
	if !errors.Is(err, ErrIdentityTranslationFailed) {
		t.Fatalf("TranslateJSON() error = %v, want %v", err, ErrIdentityTranslationFailed)
	}
}

func TestIdentityTranslationRejectsEmptyBodyWhenRequired(t *testing.T) {
	translation := mustTestTranslation(t)

	_, err := translation.TranslateJSON(nil)
	if !errors.Is(err, ErrIdentityTranslationFailed) {
		t.Fatalf("TranslateJSON() error = %v, want %v", err, ErrIdentityTranslationFailed)
	}
}

func TestIdentityTranslationRejectsIncompleteCanonicalIdentity(t *testing.T) {
	translation, err := newIdentityTranslation(identityTranslationFile{
		Enabled: true,
		Targets: []identityTargetFile{{
			CanonicalField: "target_identity",
			Required:       true,
		}},
		Mappings: []identityMappingFile{{
			LegacyIdentity: "pi-crew-planner",
			Profile:        "pi-crew-planner",
		}},
	})
	if err != nil {
		t.Fatalf("newIdentityTranslation() error = %v", err)
	}

	_, err = translation.TranslateJSON([]byte(`{"member_identity":"pi-crew-planner"}`))
	if !errors.Is(err, ErrIdentityTranslationFailed) {
		t.Fatalf("TranslateJSON() error = %v, want %v", err, ErrIdentityTranslationFailed)
	}
}

func mustTestTranslation(t *testing.T) IdentityTranslation {
	t.Helper()
	translation, err := newIdentityTranslation(identityTranslationFile{
		Enabled: true,
		Targets: []identityTargetFile{{
			CanonicalField: "target_identity",
			Required:       true,
		}},
		Mappings: []identityMappingFile{
			{LegacyIdentity: "pi-crew-planner", Profile: "pi-crew-planner"},
			{LegacyIdentity: "den-mcp-runner", Profile: "den-mcp-runner"},
		},
	})
	if err != nil {
		t.Fatalf("newIdentityTranslation() error = %v", err)
	}
	return translation
}
