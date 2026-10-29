package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const pollSecondsSetting = "poll_seconds"

type Service struct {
	store      *Store
	now        func() time.Time
	staleAfter time.Duration
}

func NewService(store *Store, now func() time.Time, staleAfter time.Duration) *Service {
	return &Service{store: store, now: now, staleAfter: staleAfter}
}

// Hosts

func (s *Service) RegisterHost(ctx context.Context, id string, request RegisterHostRequest) (HostResponse, error) {
	if err := validateID("host_id", id); err != nil {
		return HostResponse{}, err
	}
	now := s.now().UTC()
	var saved *Host
	err := s.store.InTx(ctx, func(tx *Tx) error {
		host, err := tx.Host(id)
		if err != nil {
			return err
		}
		if host == nil {
			host = &Host{ID: id, CreatedAt: now}
		}
		host.Name = firstNonEmpty(request.Name, id)
		host.Role, host.Access, host.Note, host.Position = request.Role, request.Access, request.Note, request.Position
		host.UpdatedAt = now
		saved = host
		return tx.PutHost(host)
	})
	if err != nil {
		return HostResponse{}, err
	}
	return s.hostResponse(saved, nil, now), nil
}

// ReportTelemetry records a probe snapshot, or a failed attempt that keeps the
// last good snapshot visible beside the error.
func (s *Service) ReportTelemetry(ctx context.Context, id string, request TelemetryRequest) error {
	hasTelemetry := len(request.Telemetry) > 0 && string(request.Telemetry) != "null"
	if hasTelemetry == (request.Error != nil) {
		return validationFailed("send exactly one of telemetry or error")
	}
	var probe probeSnapshot
	if hasTelemetry {
		var err error
		if probe, err = parseTelemetry(request.Telemetry); err != nil {
			return err
		}
	}
	now := s.now().UTC()
	return s.store.InTx(ctx, func(tx *Tx) error {
		host, err := requireHost(tx, id)
		if err != nil {
			return err
		}
		host.AttemptedAt, host.UpdatedAt = &now, now
		if !hasTelemetry {
			host.Error = truncate(strings.TrimSpace(*request.Error), 600)
			return tx.PutHost(host)
		}
		// An old snapshot pushed again must stay stale, and a future clock
		// must not make one look fresher than its receipt.
		sampled := now
		if probe.SampledAt != nil && probe.SampledAt.Before(now) {
			sampled = *probe.SampledAt
		}
		host.Telemetry, host.Error, host.ReceivedAt, host.SampledAt = request.Telemetry, "", &now, &sampled
		if err := tx.PutHost(host); err != nil {
			return err
		}
		found, err := tx.Resources(id)
		if err != nil {
			return err
		}
		for _, resource := range found {
			changed := false
			if present, ok := probe.Paths[resource.ProbedPath()]; ok {
				resource.PathPresent, resource.PathCheckedAt, changed = &present, &now, true
			}
			if resource.Match != "" && probe.matches(resource.Match) {
				resource.ObservedActivityAt, changed = &now, true
			}
			if changed {
				if err := tx.PutResource(resource); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ReportApps records one round of native-queue and health probes. A running
// ComfyUI queue is observed activity for the resources it names.
func (s *Service) ReportApps(ctx context.Context, id string, request AppsRequest) error {
	now := s.now().UTC()
	for i := range request.Apps {
		app := &request.Apps[i]
		if app.Name == "" || app.Kind == "" {
			return validationFailed("each app needs a name and kind")
		}
		app.CheckedAt, app.Resources, app.Jobs = now, nonNil(app.Resources), nonNil(app.Jobs)
		if app.Error != nil {
			trimmed := truncate(*app.Error, 200)
			app.Error = &trimmed
		}
	}
	return s.store.InTx(ctx, func(tx *Tx) error {
		host, err := requireHost(tx, id)
		if err != nil {
			return err
		}
		host.Apps, host.AppsCheckedAt, host.UpdatedAt = request.Apps, &now, now
		if err := tx.PutHost(host); err != nil {
			return err
		}
		for _, app := range request.Apps {
			if app.Running == nil || *app.Running == 0 {
				continue
			}
			for _, key := range app.Resources {
				resource, err := tx.Resource(key)
				if err != nil {
					return err
				}
				if resource != nil {
					resource.ObservedActivityAt = &now
					if err := tx.PutResource(resource); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (s *Service) PutSettings(ctx context.Context, request SettingsRequest) error {
	if request.PollSeconds < 1 || request.PollSeconds > 3600 {
		return validationFailed("poll_seconds must be between 1 and 3600")
	}
	return s.store.InTx(ctx, func(tx *Tx) error {
		return tx.PutSetting(pollSecondsSetting, strconv.Itoa(request.PollSeconds))
	})
}

// Directory

// UpsertResource registers or updates a resource. Usage dates are only ever
// set by touch/confirm and by reported work, never by an edit.
func (s *Service) UpsertResource(ctx context.Context, request ResourceRequest) (ResourceResponse, error) {
	if err := validateID("id", strings.TrimSpace(request.ID)); err != nil {
		return ResourceResponse{}, err
	}
	now := s.now().UTC()
	var saved *Resource
	err := s.store.InTx(ctx, func(tx *Tx) error {
		resource, err := tx.Resource(request.ID)
		if err != nil {
			return err
		}
		if resource == nil {
			resource = &Resource{ID: request.ID, Kind: defaultResourceKind, Lifecycle: LifecycleUnknown}
		} else if request.Host != nil && *request.Host != resource.Host {
			return validationFailed("resource %s belongs to %s; register a new resource for another host", resource.ID, resource.Host)
		}
		applyResourceFields(resource, request)
		if _, err := requireHost(tx, resource.Host); err != nil {
			return err
		}
		if strings.TrimSpace(resource.Name) == "" || strings.TrimSpace(resource.Path) == "" {
			return validationFailed("resource name and path are required so another user can find it")
		}
		if !resource.Lifecycle.IsValid() {
			return validationFailed("lifecycle must be maintained, experimental, retired or unknown")
		}
		owner := firstNonEmpty(strings.TrimSpace(request.Owner), "owner")
		if request.Touch {
			resource.LastUsedAt, resource.LastUsedBy = &now, owner
		}
		if request.Confirm {
			resource.ConfirmedAt, resource.ConfirmedBy = &now, owner
		}
		resource.UpdatedAt = now
		saved = resource
		return tx.PutResource(resource)
	})
	if err != nil {
		return ResourceResponse{}, err
	}
	return toResourceResponse(saved), nil
}

func applyResourceFields(r *Resource, request ResourceRequest) {
	for target, value := range map[*string]*string{
		&r.Host: request.Host, &r.Name: request.Name, &r.Path: request.Path, &r.ProbePath: request.ProbePath,
		&r.RuntimePath: request.RuntimePath, &r.Access: request.Access, &r.Sharing: request.Sharing,
		&r.Notes: request.Notes, &r.Guide: request.Guide, &r.Match: request.Match,
	} {
		if value != nil {
			*target = strings.TrimSpace(*value)
		}
	}
	if request.Kind != nil && strings.TrimSpace(*request.Kind) != "" {
		r.Kind = strings.TrimSpace(*request.Kind)
	}
	if request.Lifecycle != nil {
		r.Lifecycle = Lifecycle(firstNonEmpty(strings.TrimSpace(*request.Lifecycle), string(LifecycleUnknown)))
	}
}

func (s *Service) Resources(ctx context.Context) ([]ResourceResponse, error) {
	var found []*Resource
	err := s.store.InTx(ctx, func(tx *Tx) (err error) {
		found, err = tx.Resources("")
		return err
	})
	return toResourceResponses(found), err
}

func (s *Service) Resource(ctx context.Context, id string) (ResourceResponse, error) {
	var found *Resource
	err := s.store.InTx(ctx, func(tx *Tx) (err error) {
		found, err = tx.Resource(id)
		return err
	})
	if err != nil {
		return ResourceResponse{}, err
	}
	if found == nil {
		return ResourceResponse{}, notFound(ErrResourceNotFound, "resource_not_found", id)
	}
	return toResourceResponse(found), nil
}

// Work ledger

// ReportWork records a check-in or an application's job report. Repeated
// reports with the same source, host and external_id update one row; a
// delayed or older report never revives finished work.
func (s *Service) ReportWork(ctx context.Context, request WorkReportRequest) (WorkReceipt, error) {
	state := WorkState(firstNonEmpty(request.State, string(WorkRunning)))
	if !state.IsValid() {
		return WorkReceipt{}, validationFailed("work state must be queued, running, completed, failed or cancelled")
	}
	ttlSeconds := float64(DefaultTTLSeconds)
	if request.TTLSeconds != nil {
		ttlSeconds = *request.TTLSeconds
	}
	if ttlSeconds <= 0 || math.IsInf(ttlSeconds, 0) || math.IsNaN(ttlSeconds) {
		return WorkReceipt{}, validationFailed("a positive ttl_seconds is needed to identify lost check-ins")
	}
	ttl := time.Duration(ttlSeconds * float64(time.Second))
	source := firstNonEmpty(strings.TrimSpace(request.Source), "check-in")
	if request.ExternalID != nil && *request.ExternalID == "" {
		request.ExternalID = nil
	}
	now := s.now().UTC()
	observed := now
	if request.ObservedAt != nil {
		observed = request.ObservedAt.UTC()
	}
	var receipt WorkReceipt
	err := s.store.InTx(ctx, func(tx *Tx) error {
		if _, err := requireHost(tx, request.Host); err != nil {
			return err
		}
		var existing *Work
		if request.ExternalID != nil {
			var err error
			if existing, err = tx.WorkByExternalID(source, request.Host, *request.ExternalID); err != nil {
				return err
			}
		}
		if existing != nil && ((existing.State.IsTerminal() && state.IsActive()) || existing.UpdatedAt.After(observed)) {
			receipt = WorkReceipt{WorkResponse: toWorkResponse(existing, now), Warnings: []string{}}
			return nil
		}
		resources := request.Resources
		if resources == nil && existing != nil {
			resources = existing.Resources
		}
		resources = dedupe(resources)
		for _, key := range resources {
			resource, err := tx.Resource(key)
			if err != nil {
				return err
			}
			if resource == nil || resource.Host != request.Host {
				return validationFailed("resource %q does not belong to %s; inspect resources first", key, request.Host)
			}
		}
		work := existing
		if work == nil {
			work = &Work{ID: uuid.NewString(), Host: request.Host, Source: source, ExternalID: request.ExternalID, CreatedAt: observed}
		}
		work.Title = firstNonEmpty(strings.TrimSpace(request.Title), work.Title, "Untitled work")
		work.Owner = firstNonEmpty(strings.TrimSpace(request.Owner), work.Owner, "unknown")
		work.Project, work.Resources, work.Note, work.URL = request.Project, resources, request.Note, request.URL
		work.TTL = ttl
		applyWorkState(work, state, observed)
		if err := tx.PutWork(work); err != nil {
			return err
		}
		if err := touchResources(tx, work); err != nil {
			return err
		}
		active, err := tx.ListWork(WorkFilter{ActiveOnly: true, Host: request.Host})
		if err != nil {
			return err
		}
		warnings := []string{}
		for _, other := range active {
			if other.ID != work.ID && !other.Stale(observed) {
				warnings = append(warnings, fmt.Sprintf("Also reported on %s: %s (%s)", request.Host, other.Title, other.Owner))
			}
		}
		receipt = WorkReceipt{WorkResponse: toWorkResponse(work, now), Warnings: warnings}
		return nil
	})
	return receipt, err
}

// UpdateWork renews a row ({}), or checks it out with a terminal state. A
// finished row is returned unchanged, so a late heartbeat cannot revive it.
func (s *Service) UpdateWork(ctx context.Context, id string, request WorkUpdateRequest) (WorkResponse, error) {
	now := s.now().UTC()
	observed := now
	if request.ObservedAt != nil {
		observed = request.ObservedAt.UTC()
	}
	var result WorkResponse
	err := s.store.InTx(ctx, func(tx *Tx) error {
		work, err := tx.Work(id)
		if err != nil {
			return err
		}
		if work == nil {
			return notFound(ErrWorkNotFound, "work_not_found", id)
		}
		state := work.State
		if request.State != nil {
			state = WorkState(*request.State)
		}
		if !state.IsValid() {
			return validationFailed("work state must be queued, running, completed, failed or cancelled")
		}
		if work.State.IsTerminal() || work.UpdatedAt.After(observed) {
			result = toWorkResponse(work, now)
			return nil
		}
		if request.Note != nil {
			work.Note = *request.Note
		}
		applyWorkState(work, state, observed)
		if err := tx.PutWork(work); err != nil {
			return err
		}
		result = toWorkResponse(work, now)
		return touchResources(tx, work)
	})
	return result, err
}

func applyWorkState(work *Work, state WorkState, at time.Time) {
	work.State, work.UpdatedAt, work.ExpiresAt = state, at, at.Add(work.TTL)
	if state == WorkRunning && work.StartedAt == nil {
		work.StartedAt = &at
	}
	switch {
	case state.IsTerminal() && work.EndedAt == nil:
		work.EndedAt = &at
	case state.IsActive():
		work.EndedAt = nil
	}
}

// touchResources records actual use. Queued work, and a cancel of work that
// never started, did not use anything.
func touchResources(tx *Tx, work *Work) error {
	if work.State == WorkQueued || (work.State == WorkCancelled && work.StartedAt == nil) {
		return nil
	}
	usedAt := work.UpdatedAt
	if work.EndedAt != nil {
		usedAt = *work.EndedAt
	}
	for _, key := range work.Resources {
		resource, err := tx.Resource(key)
		if err != nil {
			return err
		}
		if resource == nil || (resource.LastUsedAt != nil && usedAt.Before(*resource.LastUsedAt)) {
			continue
		}
		resource.LastUsedAt, resource.LastUsedBy = &usedAt, work.Owner
		if err := tx.PutResource(resource); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListWork(ctx context.Context, filter WorkFilter) ([]WorkResponse, error) {
	now := s.now().UTC()
	var found []*Work
	err := s.store.InTx(ctx, func(tx *Tx) (err error) {
		found, err = tx.ListWork(filter)
		return err
	})
	return toWorkResponses(found, now), err
}

func (s *Service) Work(ctx context.Context, id string) (WorkResponse, error) {
	var found *Work
	err := s.store.InTx(ctx, func(tx *Tx) (err error) {
		found, err = tx.Work(id)
		return err
	})
	if err != nil {
		return WorkResponse{}, err
	}
	if found == nil {
		return WorkResponse{}, notFound(ErrWorkNotFound, "work_not_found", id)
	}
	return toWorkResponse(found, s.now().UTC()), nil
}

// State

func (s *Service) State(ctx context.Context, workLimit int) (StateResponse, error) {
	now := s.now().UTC()
	var hosts []*Host
	var found []*Resource
	var work []*Work
	pollSeconds := DefaultPollSeconds
	err := s.store.InTx(ctx, func(tx *Tx) error {
		var err error
		if hosts, err = tx.Hosts(); err != nil {
			return err
		}
		if found, err = tx.Resources(""); err != nil {
			return err
		}
		if work, err = tx.ListWork(WorkFilter{Limit: workLimit}); err != nil {
			return err
		}
		if value, ok, err := tx.Setting(pollSecondsSetting); err != nil {
			return err
		} else if ok {
			pollSeconds, _ = strconv.Atoi(value)
		}
		return nil
	})
	if err != nil {
		return StateResponse{}, err
	}
	response := StateResponse{
		GeneratedAt: now, PollSeconds: pollSeconds, StaleAfterSeconds: int(s.staleAfter.Seconds()),
		Hosts: make([]HostResponse, 0, len(hosts)), Resources: toResourceResponses(found),
		Work: toWorkResponses(work, now),
	}
	response.Counts.Hosts, response.Counts.Resources = len(hosts), len(found)
	for _, host := range hosts {
		view := s.hostResponse(host, work, now)
		response.Hosts = append(response.Hosts, view)
		if !view.Stale && view.Error == nil {
			response.Counts.HostsFresh++
		}
		for _, app := range view.Apps {
			if !app.Stale && app.Online && app.Queued != nil {
				response.Counts.NativeQueued += *app.Queued
			}
		}
	}
	for _, w := range work {
		if w.State == WorkRunning && !w.Stale(now) {
			response.Counts.Running++
		}
	}
	return response, nil
}

// hostResponse judges one host. Stale or failed telemetry is unknown, never
// quiet; measured load, a fresh running queue or a running report is working.
func (s *Service) hostResponse(host *Host, work []*Work, now time.Time) HostResponse {
	stale := host.SampledAt == nil || now.Sub(*host.SampledAt) > s.staleAfter
	appsFresh := host.AppsCheckedAt != nil && now.Sub(*host.AppsCheckedAt) <= s.staleAfter
	apps := make([]AppResponse, 0, len(host.Apps))
	busy, nativeQueued := telemetryBusy(host.Telemetry), false
	for _, app := range host.Apps {
		apps = append(apps, AppResponse{App: app, Stale: now.Sub(app.CheckedAt) > s.staleAfter})
		if appsFresh && app.Running != nil && *app.Running > 0 {
			busy = true
		}
		if appsFresh && app.Queued != nil && *app.Queued > 0 {
			nativeQueued = true
		}
	}
	reportedRunning, reportedActive := false, false
	for _, w := range work {
		if w.Host == host.ID && w.State.IsActive() && !w.Stale(now) {
			reportedActive = true
			reportedRunning = reportedRunning || w.State == WorkRunning
		}
	}
	status := HostQuiet
	switch {
	case stale || host.Error != "":
		status = HostUnknown
	case busy || reportedRunning:
		status = HostWorking
	case reportedActive || nativeQueued:
		status = HostQueued
	}
	var hostError *string
	if host.Error != "" {
		hostError = &host.Error
	}
	telemetry := host.Telemetry
	if len(telemetry) == 0 {
		telemetry = json.RawMessage("null")
	}
	return HostResponse{
		ID: host.ID, Name: host.Name, Role: host.Role, Access: host.Access, Note: host.Note, Position: host.Position,
		Status: string(status), Stale: stale, Error: hostError, AttemptedAt: host.AttemptedAt,
		SampledAt: host.SampledAt, ReceivedAt: host.ReceivedAt, Telemetry: telemetry, Apps: apps,
		AppsCheckedAt: host.AppsCheckedAt,
	}
}

func telemetryBusy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var snapshot struct {
		CPUPercent *float64 `json:"cpu_percent"`
		GPUs       []struct {
			Utilization *float64 `json:"utilization_percent"`
		} `json:"gpus"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return false
	}
	if snapshot.CPUPercent != nil && *snapshot.CPUPercent > busyCPUPercent {
		return true
	}
	return slices.ContainsFunc(snapshot.GPUs, func(gpu struct {
		Utilization *float64 `json:"utilization_percent"`
	},
	) bool {
		return gpu.Utilization != nil && *gpu.Utilization > busyGPUPercent
	})
}

func requireHost(tx *Tx, id string) (*Host, error) {
	if id == "" {
		return nil, validationFailed("host is required")
	}
	host, err := tx.Host(id)
	if err != nil {
		return nil, err
	}
	if host == nil {
		// Prevent a typo from recording activity against an imaginary machine.
		hosts, err := tx.Hosts()
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(hosts))
		for _, known := range hosts {
			names = append(names, known.ID)
		}
		return nil, &ServiceError{
			err:  fmt.Errorf("%w: %q; choose one of %s", ErrHostNotFound, id, strings.Join(names, ", ")),
			code: "host_not_found", status: http.StatusNotFound,
		}
	}
	return host, nil
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// truncate keeps the end of value, where command errors say what failed.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
