package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"den-services/shared/identity"
)

var (
	ErrIdentityTranslationFailed = errors.New("identity translation failed") //nolint:gochecknoglobals
)

type IdentityTranslation struct {
	enabled  bool
	targets  []IdentityTranslationTarget
	mappings map[string]IdentityMapping
}

type IdentityTranslationTarget struct {
	canonicalField string
	required       bool
	profileFields  []string
	instanceFields []string
	sessionFields  []string
}

type IdentityMapping struct {
	legacyIdentity string
	profile        identity.ProfileIdentity
	instanceID     identity.AgentInstanceID
	session        *identity.SessionKey
}

type identityTranslationFile struct {
	Enabled  bool                  `yaml:"enabled"`
	Targets  []identityTargetFile  `yaml:"targets"`
	Mappings []identityMappingFile `yaml:"mappings"`
}

type identityTargetFile struct {
	CanonicalField string   `yaml:"canonical_field"`
	Required       bool     `yaml:"required"`
	ProfileFields  []string `yaml:"profile_fields"`
	InstanceFields []string `yaml:"instance_fields"`
	SessionFields  []string `yaml:"session_fields"`
}

type identityMappingFile struct {
	LegacyIdentity string `yaml:"legacy_identity"`
	Profile        string `yaml:"profile"`
	InstanceID     string `yaml:"instance_id"`
	SessionKey     string `yaml:"session_key"`
}

func newIdentityTranslation(file identityTranslationFile) (IdentityTranslation, error) {
	if !file.Enabled {
		return IdentityTranslation{}, nil
	}
	if len(file.Targets) == 0 {
		return IdentityTranslation{}, errors.New("identity_translation.targets is required")
	}
	if len(file.Mappings) == 0 {
		return IdentityTranslation{}, errors.New("identity_translation.mappings is required")
	}
	targets := make([]IdentityTranslationTarget, 0, len(file.Targets))
	for _, targetFile := range file.Targets {
		target, err := newIdentityTranslationTarget(targetFile)
		if err != nil {
			return IdentityTranslation{}, err
		}
		targets = append(targets, target)
	}
	mappings := make(map[string]IdentityMapping, len(file.Mappings))
	for _, mappingFile := range file.Mappings {
		mapping, err := newIdentityMapping(mappingFile)
		if err != nil {
			return IdentityTranslation{}, err
		}
		if _, exists := mappings[mapping.legacyIdentity]; exists {
			return IdentityTranslation{}, fmt.Errorf("duplicate legacy identity mapping: %s", mapping.legacyIdentity)
		}
		mappings[mapping.legacyIdentity] = mapping
	}
	return IdentityTranslation{
		enabled:  true,
		targets:  targets,
		mappings: mappings,
	}, nil
}

func newIdentityTranslationTarget(file identityTargetFile) (IdentityTranslationTarget, error) {
	if strings.TrimSpace(file.CanonicalField) == "" {
		return IdentityTranslationTarget{}, errors.New("identity_translation target canonical_field is required")
	}
	return IdentityTranslationTarget{
		canonicalField: strings.TrimSpace(file.CanonicalField),
		required:       file.Required,
		profileFields:  configuredFields(file.ProfileFields, defaultProfileFields()),
		instanceFields: configuredFields(file.InstanceFields, defaultInstanceFields()),
		sessionFields:  configuredFields(file.SessionFields, defaultSessionFields()),
	}, nil
}

func defaultProfileFields() []string {
	return []string{"member_identity", "agent_identity"}
}

func defaultInstanceFields() []string {
	return []string{"agent_instance_id", "concrete_identity"}
}

func defaultSessionFields() []string {
	return []string{"session_key", "hermes_session_key"}
}

func newIdentityMapping(file identityMappingFile) (IdentityMapping, error) {
	if strings.TrimSpace(file.LegacyIdentity) == "" {
		return IdentityMapping{}, errors.New("identity mapping legacy_identity is required")
	}
	if strings.TrimSpace(file.Profile) == "" {
		return IdentityMapping{}, fmt.Errorf("identity mapping %s profile is required", file.LegacyIdentity)
	}
	var session *identity.SessionKey
	if strings.TrimSpace(file.SessionKey) != "" {
		value := identity.SessionKey(strings.TrimSpace(file.SessionKey))
		session = &value
	}
	return IdentityMapping{
		legacyIdentity: strings.TrimSpace(file.LegacyIdentity),
		profile:        identity.ProfileIdentity(strings.TrimSpace(file.Profile)),
		instanceID:     identity.AgentInstanceID(strings.TrimSpace(file.InstanceID)),
		session:        session,
	}, nil
}

func configuredFields(configured []string, defaults []string) []string {
	if len(configured) == 0 {
		return append([]string{}, defaults...)
	}
	fields := make([]string, 0, len(configured))
	for _, field := range configured {
		if strings.TrimSpace(field) != "" {
			fields = append(fields, strings.TrimSpace(field))
		}
	}
	return fields
}

func (t IdentityTranslation) Enabled() bool {
	return t.enabled
}

func (t IdentityTranslation) TranslateJSON(body []byte) ([]byte, error) {
	if !t.enabled {
		return body, nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if t.hasRequiredTarget() {
			return nil, fmt.Errorf("%w: request body is required for identity translation", ErrIdentityTranslationFailed)
		}
		return body, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decoding json body: %w", ErrIdentityTranslationFailed, err)
	}
	for _, target := range t.targets {
		translated, matched, err := t.translateTarget(payload, target)
		if err != nil {
			return nil, err
		}
		if !matched {
			if target.required {
				return nil, fmt.Errorf("%w: missing legacy identity fields for %s", ErrIdentityTranslationFailed, target.canonicalField)
			}
			continue
		}
		encoded, err := json.Marshal(translated)
		if err != nil {
			return nil, fmt.Errorf("%w: encoding %s: %w", ErrIdentityTranslationFailed, target.canonicalField, err)
		}
		payload[target.canonicalField] = encoded
		target.removeLegacyFields(payload)
	}
	translatedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding json body: %w", ErrIdentityTranslationFailed, err)
	}
	return translatedBody, nil
}

func (t IdentityTranslation) hasRequiredTarget() bool {
	for _, target := range t.targets {
		if target.required {
			return true
		}
	}
	return false
}

func (t IdentityTranslation) translateTarget(payload map[string]json.RawMessage, target IdentityTranslationTarget) (identity.AgentIdentity, bool, error) {
	legacyProfile, matched, err := firstString(payload, target.profileFields)
	if err != nil {
		return identity.AgentIdentity{}, false, err
	}
	if !matched {
		return identity.AgentIdentity{}, false, nil
	}
	mapping, ok := t.mappings[legacyProfile]
	if !ok {
		return identity.AgentIdentity{}, false, fmt.Errorf("%w: unknown legacy identity %s", ErrIdentityTranslationFailed, legacyProfile)
	}
	instanceID := mapping.instanceID
	if explicitInstance, ok, err := firstString(payload, target.instanceFields); err != nil {
		return identity.AgentIdentity{}, false, err
	} else if ok {
		instanceID = identity.AgentInstanceID(explicitInstance)
	}
	session := mapping.session
	if explicitSession, ok, err := firstString(payload, target.sessionFields); err != nil {
		return identity.AgentIdentity{}, false, err
	} else if ok {
		value := identity.SessionKey(explicitSession)
		session = &value
	}
	translated, err := identity.NewAgentIdentity(mapping.profile, instanceID, session)
	if err != nil {
		return identity.AgentIdentity{}, false, fmt.Errorf("%w: incomplete canonical identity for %s", ErrIdentityTranslationFailed, legacyProfile)
	}
	return translated, true, nil
}

func firstString(payload map[string]json.RawMessage, fields []string) (string, bool, error) {
	for _, field := range fields {
		raw, ok := payload[field]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", false, fmt.Errorf("%w: %s must be a string", ErrIdentityTranslationFailed, field)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", false, fmt.Errorf("%w: %s must not be empty", ErrIdentityTranslationFailed, field)
		}
		return value, true, nil
	}
	return "", false, nil
}

func (t IdentityTranslationTarget) removeLegacyFields(payload map[string]json.RawMessage) {
	for _, field := range append(append([]string{}, t.profileFields...), append(t.instanceFields, t.sessionFields...)...) {
		delete(payload, field)
	}
}
