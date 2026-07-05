package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"den-services/shared/identity"
)

type NoopWakeTargetResolver struct{}

func (NoopWakeTargetResolver) ResolveWakeTargets(_ context.Context, _ []*ChannelMembership) error {
	return nil
}

type RuntimeWakeTargetResolver struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

type runtimeInstanceDTO struct {
	InstanceID      identity.AgentInstanceID `json:"instance_id"`
	ProfileIdentity identity.ProfileIdentity `json:"profile_identity"`
	State           string                   `json:"state"`
	StartedAt       time.Time                `json:"started_at"`
	LastHeartbeatAt *time.Time               `json:"last_heartbeat_at,omitempty"`
}

func NewRuntimeWakeTargetResolver(baseURL string, serviceToken string, timeout time.Duration) *RuntimeWakeTargetResolver {
	return &RuntimeWakeTargetResolver{
		baseURL:      strings.TrimRight(baseURL, "/"),
		serviceToken: strings.TrimSpace(serviceToken),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (r *RuntimeWakeTargetResolver) ResolveWakeTargets(ctx context.Context, memberships []*ChannelMembership) error {
	profiles := wakeableProfiles(memberships)
	if len(profiles) == 0 {
		return nil
	}
	targets, err := r.listWakeTargets(ctx, profiles)
	if err != nil {
		return nil
	}
	for _, membership := range memberships {
		if profile := membershipWakeProfile(membership); profile != "" {
			membership.WakeTarget = targets[profile]
		}
	}
	return nil
}

func (r *RuntimeWakeTargetResolver) listWakeTargets(ctx context.Context, profiles map[identity.ProfileIdentity]struct{}) (map[identity.ProfileIdentity]*identity.AgentIdentity, error) {
	if r.baseURL == "" || r.serviceToken == "" {
		return nil, fmt.Errorf("runtime wake target resolver is missing base URL or service token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/v1/runtime/instances", nil)
	if err != nil {
		return nil, fmt.Errorf("creating runtime instances request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.serviceToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing runtime instances for wake targets: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runtime instances status: %s", resp.Status)
	}
	var instances []runtimeInstanceDTO
	if err := json.NewDecoder(resp.Body).Decode(&instances); err != nil {
		return nil, fmt.Errorf("decoding runtime instances: %w", err)
	}
	targets := make(map[identity.ProfileIdentity]*identity.AgentIdentity, len(profiles))
	selected := make(map[identity.ProfileIdentity]runtimeInstanceDTO, len(profiles))
	for _, instance := range instances {
		if _, ok := profiles[instance.ProfileIdentity]; !ok || !runtimeStateCanWake(instance.State) || !instance.InstanceID.IsValid() {
			continue
		}
		if current, exists := selected[instance.ProfileIdentity]; exists && !instanceIsNewer(instance, current) {
			continue
		}
		target := identity.AgentIdentity{
			Profile:    instance.ProfileIdentity,
			InstanceID: instance.InstanceID,
		}
		selected[instance.ProfileIdentity] = instance
		targets[instance.ProfileIdentity] = &target
	}
	return targets, nil
}

func wakeableProfiles(memberships []*ChannelMembership) map[identity.ProfileIdentity]struct{} {
	profiles := make(map[identity.ProfileIdentity]struct{})
	for _, membership := range memberships {
		if profile := membershipWakeProfile(membership); profile != "" {
			profiles[profile] = struct{}{}
		}
	}
	return profiles
}

func membershipWakeProfile(membership *ChannelMembership) identity.ProfileIdentity {
	if membership == nil ||
		membership.MemberType != "agent" ||
		membership.MembershipStatus == "left" ||
		membership.WakePolicy == "never" ||
		membership.ProfileIdentity == nil ||
		strings.TrimSpace(*membership.ProfileIdentity) == "" {
		return ""
	}
	return identity.ProfileIdentity(strings.TrimSpace(*membership.ProfileIdentity))
}

func runtimeStateCanWake(state string) bool {
	switch state {
	case "active", "idle", "busy":
		return true
	default:
		return false
	}
}

func instanceIsNewer(candidate runtimeInstanceDTO, current runtimeInstanceDTO) bool {
	candidateTime := candidate.StartedAt
	if candidate.LastHeartbeatAt != nil {
		candidateTime = *candidate.LastHeartbeatAt
	}
	currentTime := current.StartedAt
	if current.LastHeartbeatAt != nil {
		currentTime = *current.LastHeartbeatAt
	}
	return candidateTime.After(currentTime)
}
