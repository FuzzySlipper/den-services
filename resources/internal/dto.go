package resources

import (
	"encoding/json"
	"time"
)

// RegisterHostRequest is the body of PUT /v1/resources/hosts/{host_id}.
type RegisterHostRequest struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Access   string `json:"access"`
	Note     string `json:"note"`
	Position int    `json:"position"`
}

// TelemetryRequest is a probe snapshot or the reason collection failed.
type TelemetryRequest struct {
	Telemetry json.RawMessage `json:"telemetry"`
	Error     *string         `json:"error"`
}

type AppsRequest struct {
	Apps []App `json:"apps"`
}

type SettingsRequest struct {
	PollSeconds int `json:"poll_seconds"`
}

// ResourceRequest registers or updates a resource. Fields left out keep their
// stored values.
type ResourceRequest struct {
	ID          string  `json:"id"`
	Host        *string `json:"host"`
	Name        *string `json:"name"`
	Kind        *string `json:"kind"`
	Path        *string `json:"path"`
	ProbePath   *string `json:"probe_path"`
	RuntimePath *string `json:"runtime_path"`
	Access      *string `json:"access"`
	Sharing     *string `json:"sharing"`
	Lifecycle   *string `json:"lifecycle"`
	Notes       *string `json:"notes"`
	Guide       *string `json:"guide"`
	Match       *string `json:"match"`
	Touch       bool    `json:"touch"`
	Confirm     bool    `json:"confirm"`
	Owner       string  `json:"owner"`
}

type WorkReportRequest struct {
	Host       string     `json:"host"`
	Title      string     `json:"title"`
	Owner      string     `json:"owner"`
	Project    string     `json:"project"`
	Resources  []string   `json:"resources"`
	State      string     `json:"state"`
	TTLSeconds *float64   `json:"ttl_seconds"`
	Source     string     `json:"source"`
	ExternalID *string    `json:"external_id"`
	Note       string     `json:"note"`
	URL        string     `json:"url"`
	ObservedAt *time.Time `json:"observed_at"`
}

type WorkUpdateRequest struct {
	State      *string    `json:"state"`
	Note       *string    `json:"note"`
	ObservedAt *time.Time `json:"observed_at"`
}

type HostResponse struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Role          string          `json:"role"`
	Access        string          `json:"access"`
	Note          string          `json:"note"`
	Position      int             `json:"position"`
	Status        string          `json:"status"`
	Stale         bool            `json:"stale"`
	Error         *string         `json:"error"`
	AttemptedAt   *time.Time      `json:"attempted_at"`
	SampledAt     *time.Time      `json:"sampled_at"`
	ReceivedAt    *time.Time      `json:"received_at"`
	Telemetry     json.RawMessage `json:"telemetry"`
	Apps          []AppResponse   `json:"apps"`
	AppsCheckedAt *time.Time      `json:"apps_checked_at"`
}

type AppResponse struct {
	App
	Stale bool `json:"stale"`
}

type ResourceResponse struct {
	ID                 string     `json:"id"`
	Host               string     `json:"host"`
	Name               string     `json:"name"`
	Kind               string     `json:"kind"`
	Path               string     `json:"path"`
	ProbePath          string     `json:"probe_path"`
	RuntimePath        string     `json:"runtime_path"`
	Access             string     `json:"access"`
	Sharing            string     `json:"sharing"`
	Lifecycle          string     `json:"lifecycle"`
	Notes              string     `json:"notes"`
	Guide              string     `json:"guide"`
	Match              string     `json:"match"`
	UpdatedAt          time.Time  `json:"updated_at"`
	LastUsedAt         *time.Time `json:"last_used_at"`
	LastUsedBy         string     `json:"last_used_by"`
	ObservedActivityAt *time.Time `json:"observed_activity_at"`
	ConfirmedAt        *time.Time `json:"confirmed_at"`
	ConfirmedBy        string     `json:"confirmed_by"`
	PathPresent        *bool      `json:"path_present"`
	PathCheckedAt      *time.Time `json:"path_checked_at"`
}

type WorkResponse struct {
	ID         string     `json:"id"`
	Host       string     `json:"host"`
	Source     string     `json:"source"`
	ExternalID *string    `json:"external_id"`
	Title      string     `json:"title"`
	Owner      string     `json:"owner"`
	Project    string     `json:"project"`
	Resources  []string   `json:"resources"`
	State      string     `json:"state"`
	Note       string     `json:"note"`
	URL        string     `json:"url"`
	TTLSeconds float64    `json:"ttl_seconds"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	EndedAt    *time.Time `json:"ended_at"`
	Stale      bool       `json:"stale"`
}

// WorkReceipt is a report's result: the row and any overlapping reports.
type WorkReceipt struct {
	WorkResponse
	Warnings []string `json:"warnings"`
}

type StateCounts struct {
	Hosts        int `json:"hosts"`
	HostsFresh   int `json:"hosts_fresh"`
	Running      int `json:"running"`
	NativeQueued int `json:"native_queued"`
	Resources    int `json:"resources"`
}

type StateResponse struct {
	GeneratedAt       time.Time          `json:"generated_at"`
	PollSeconds       int                `json:"poll_seconds"`
	StaleAfterSeconds int                `json:"stale_after_seconds"`
	Counts            StateCounts        `json:"counts"`
	Hosts             []HostResponse     `json:"hosts"`
	Resources         []ResourceResponse `json:"resources"`
	Work              []WorkResponse     `json:"work"`
}

type DirectoryResponse struct {
	Resources []ResourceResponse `json:"resources"`
}

type WorkListResponse struct {
	Work []WorkResponse `json:"work"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

func toResourceResponse(r *Resource) ResourceResponse {
	return ResourceResponse{
		ID: r.ID, Host: r.Host, Name: r.Name, Kind: r.Kind, Path: r.Path, ProbePath: r.ProbePath,
		RuntimePath: r.RuntimePath, Access: r.Access, Sharing: r.Sharing, Lifecycle: string(r.Lifecycle),
		Notes: r.Notes, Guide: r.Guide, Match: r.Match, UpdatedAt: r.UpdatedAt, LastUsedAt: r.LastUsedAt,
		LastUsedBy: r.LastUsedBy, ObservedActivityAt: r.ObservedActivityAt, ConfirmedAt: r.ConfirmedAt,
		ConfirmedBy: r.ConfirmedBy, PathPresent: r.PathPresent, PathCheckedAt: r.PathCheckedAt,
	}
}

func toResourceResponses(found []*Resource) []ResourceResponse {
	responses := make([]ResourceResponse, 0, len(found))
	for _, r := range found {
		responses = append(responses, toResourceResponse(r))
	}
	return responses
}

func toWorkResponse(w *Work, now time.Time) WorkResponse {
	return WorkResponse{
		ID: w.ID, Host: w.Host, Source: w.Source, ExternalID: w.ExternalID, Title: w.Title, Owner: w.Owner,
		Project: w.Project, Resources: nonNil(w.Resources), State: string(w.State), Note: w.Note, URL: w.URL,
		TTLSeconds: w.TTL.Seconds(), CreatedAt: w.CreatedAt, StartedAt: w.StartedAt, UpdatedAt: w.UpdatedAt,
		ExpiresAt: w.ExpiresAt, EndedAt: w.EndedAt, Stale: w.Stale(now),
	}
}

func toWorkResponses(found []*Work, now time.Time) []WorkResponse {
	responses := make([]WorkResponse, 0, len(found))
	for _, w := range found {
		responses = append(responses, toWorkResponse(w, now))
	}
	return responses
}
