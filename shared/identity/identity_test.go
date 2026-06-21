package identity

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAgentIdentityIsValid(t *testing.T) {
	session := SessionKey("sess-123")

	tests := []struct {
		name     string
		identity AgentIdentity
		want     bool
	}{
		{
			name: "valid identity without session",
			identity: AgentIdentity{
				Profile:    ProfileIdentity("planner"),
				InstanceID: AgentInstanceID("planner@host-1"),
			},
			want: true,
		},
		{
			name: "valid identity with session",
			identity: AgentIdentity{
				Profile:    ProfileIdentity("planner"),
				InstanceID: AgentInstanceID("planner@host-1"),
				Session:    &session,
			},
			want: true,
		},
		{
			name: "missing profile",
			identity: AgentIdentity{
				InstanceID: AgentInstanceID("planner@host-1"),
			},
			want: false,
		},
		{
			name: "missing instance",
			identity: AgentIdentity{
				Profile: ProfileIdentity("planner"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.identity.IsValid(); got != tt.want {
				t.Fatalf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewAgentIdentityValidates(t *testing.T) {
	_, err := NewAgentIdentity("", "instance", nil)
	if !errors.Is(err, ErrInvalidAgentIdentity) {
		t.Fatalf("NewAgentIdentity() error = %v, want %v", err, ErrInvalidAgentIdentity)
	}
}

func TestAgentIdentityJSONShape(t *testing.T) {
	session := SessionKey("sess-123")
	identity := AgentIdentity{
		Profile:    ProfileIdentity("planner"),
		InstanceID: AgentInstanceID("planner@host-1"),
		Session:    &session,
	}

	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"profile":"planner","instance_id":"planner@host-1","session_key":"sess-123"}`
	if string(data) != want {
		t.Fatalf("Marshal() = %s, want %s", data, want)
	}
}

func TestAgentIdentityEqualComparesSessionValue(t *testing.T) {
	leftSession := SessionKey("sess-123")
	rightSession := SessionKey("sess-123")
	left := AgentIdentity{
		Profile:    ProfileIdentity("planner"),
		InstanceID: AgentInstanceID("planner@host-1"),
		Session:    &leftSession,
	}
	right := AgentIdentity{
		Profile:    ProfileIdentity("planner"),
		InstanceID: AgentInstanceID("planner@host-1"),
		Session:    &rightSession,
	}

	if !left.Equal(right) {
		t.Fatal("Equal() = false, want true for matching session values")
	}

	otherSession := SessionKey("sess-other")
	mismatch := AgentIdentity{
		Profile:    ProfileIdentity("planner"),
		InstanceID: AgentInstanceID("planner@host-1"),
		Session:    &otherSession,
	}
	if left.Equal(mismatch) {
		t.Fatal("Equal() = true, want false for different session values")
	}

	withoutSession := AgentIdentity{
		Profile:    ProfileIdentity("planner"),
		InstanceID: AgentInstanceID("planner@host-1"),
	}
	if left.Equal(withoutSession) {
		t.Fatal("Equal() = true, want false when only one identity has a session")
	}
	if !withoutSession.Equal(AgentIdentity{Profile: ProfileIdentity("planner"), InstanceID: AgentInstanceID("planner@host-1")}) {
		t.Fatal("Equal() = false, want true for matching identities without session")
	}
}
