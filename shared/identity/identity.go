package identity

import "strings"

type ProfileIdentity string

func (p ProfileIdentity) IsValid() bool {
	return strings.TrimSpace(string(p)) != ""
}

func (p ProfileIdentity) String() string {
	return string(p)
}

type AgentInstanceID string

func (i AgentInstanceID) IsValid() bool {
	return strings.TrimSpace(string(i)) != ""
}

func (i AgentInstanceID) String() string {
	return string(i)
}

type SessionKey string

func (s SessionKey) IsValid() bool {
	return strings.TrimSpace(string(s)) != ""
}

func (s SessionKey) String() string {
	return string(s)
}

type AgentIdentity struct {
	Profile    ProfileIdentity `json:"profile"`
	InstanceID AgentInstanceID `json:"instance_id"`
	Session    *SessionKey     `json:"session_key,omitempty"`
}

func NewAgentIdentity(profile ProfileIdentity, instanceID AgentInstanceID, session *SessionKey) (AgentIdentity, error) {
	identity := AgentIdentity{
		Profile:    profile,
		InstanceID: instanceID,
		Session:    session,
	}
	if !identity.IsValid() {
		return AgentIdentity{}, ErrInvalidAgentIdentity
	}
	return identity, nil
}

func (a AgentIdentity) IsValid() bool {
	if !a.Profile.IsValid() || !a.InstanceID.IsValid() {
		return false
	}
	if a.Session != nil && !a.Session.IsValid() {
		return false
	}
	return true
}
