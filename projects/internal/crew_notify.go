package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// CrewNotify names the crew addresses to tell about a project's tasks. Den
// stores the list and checks its shape only; crew-review reads it through
// get_project and sends the notices. Later events (on_changes_requested,
// on_blocked) get their own keys.
type CrewNotify struct {
	OnDone []CrewNotifyTarget `json:"on_done,omitempty"`
}

// CrewNotifyTarget is one crew-messaging address and how its notice wakes it.
type CrewNotifyTarget struct {
	Address string `json:"address"`
	Policy  string `json:"policy"`
}

const (
	CrewNotifyEventOnDone = "on_done"
	CrewNotifyNoWake      = "no_wake"
	CrewNotifyNextTurn    = "next_turn"
	MaxCrewNotifyTargets  = 16
)

var (
	ErrInvalidCrewNotify = errors.New("invalid crew_notify")

	// A crew seat (`scope.name`) or a crew session (`s/<hex>`), as
	// crew-messaging writes them.
	crewSeatAddress    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9-]*$`)
	crewSessionAddress = regexp.MustCompile(`^s/[0-9a-f]{8,64}$`)
)

func (c CrewNotify) IsEmpty() bool {
	return len(c.OnDone) == 0
}

// UpdateCrewNotifyRequest edits one event's list: Remove drops addresses, then
// Add inserts or replaces by address, so an agent adds its own seat in one call.
type UpdateCrewNotifyRequest struct {
	Event  string             `json:"event,omitempty"`
	Add    []CrewNotifyTarget `json:"add,omitempty"`
	Remove []string           `json:"remove,omitempty"`
}

func (r UpdateCrewNotifyRequest) apply(current CrewNotify) (CrewNotify, error) {
	event := strings.TrimSpace(r.Event)
	if event == "" {
		event = CrewNotifyEventOnDone
	}
	if event != CrewNotifyEventOnDone {
		return CrewNotify{}, fmt.Errorf("%w: event must be %s", ErrInvalidCrewNotify, CrewNotifyEventOnDone)
	}
	if len(r.Add) == 0 && len(r.Remove) == 0 {
		return CrewNotify{}, fmt.Errorf("%w: add or remove at least one address", ErrInvalidCrewNotify)
	}
	removed := make(map[string]bool, len(r.Remove))
	for _, address := range r.Remove {
		removed[strings.TrimSpace(address)] = true
	}
	targets := make([]CrewNotifyTarget, 0, len(current.OnDone)+len(r.Add))
	for _, target := range current.OnDone {
		if !removed[target.Address] {
			targets = append(targets, target)
		}
	}
	for _, add := range r.Add {
		target, err := normalizeCrewNotifyTarget(add)
		if err != nil {
			return CrewNotify{}, err
		}
		replaced := false
		for index := range targets {
			if targets[index].Address == target.Address {
				targets[index], replaced = target, true
			}
		}
		if !replaced {
			targets = append(targets, target)
		}
	}
	if len(targets) > MaxCrewNotifyTargets {
		return CrewNotify{}, fmt.Errorf("%w: at most %d addresses per event", ErrInvalidCrewNotify, MaxCrewNotifyTargets)
	}
	return CrewNotify{OnDone: targets}, nil
}

func normalizeCrewNotifyTarget(target CrewNotifyTarget) (CrewNotifyTarget, error) {
	address := strings.TrimSpace(target.Address)
	if !crewSeatAddress.MatchString(address) && !crewSessionAddress.MatchString(address) {
		return CrewNotifyTarget{}, fmt.Errorf("%w: address %q is not a crew seat (scope.name) or session (s/<hex>)", ErrInvalidCrewNotify, address)
	}
	policy := strings.TrimSpace(target.Policy)
	if policy == "" {
		policy = CrewNotifyNoWake
	}
	if policy != CrewNotifyNoWake && policy != CrewNotifyNextTurn {
		return CrewNotifyTarget{}, fmt.Errorf("%w: policy must be %s or %s", ErrInvalidCrewNotify, CrewNotifyNoWake, CrewNotifyNextTurn)
	}
	return CrewNotifyTarget{Address: address, Policy: policy}, nil
}

func decodeCrewNotify(raw []byte) (CrewNotify, error) {
	var notify CrewNotify
	if len(raw) == 0 {
		return notify, nil
	}
	if err := json.Unmarshal(raw, &notify); err != nil {
		return CrewNotify{}, fmt.Errorf("decoding crew_notify: %w", err)
	}
	return notify, nil
}

func encodeCrewNotify(notify CrewNotify) ([]byte, error) {
	if notify.IsEmpty() {
		return nil, nil
	}
	data, err := json.Marshal(notify)
	if err != nil {
		return nil, fmt.Errorf("encoding crew_notify: %w", err)
	}
	return data, nil
}
