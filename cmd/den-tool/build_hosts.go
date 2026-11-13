package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

type buildResourcesState struct {
	Hosts []buildHostSample `json:"hosts"`
	Work  []buildWorkSample `json:"work"`
}

type buildHostSample struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Stale     bool   `json:"stale"`
	Error     string `json:"error"`
	Telemetry struct {
		CPUPercent *float64 `json:"cpu_percent"`
		GPUs       []struct {
			Utilization *float64 `json:"utilization_percent"`
		} `json:"gpus"`
	} `json:"telemetry"`
	Apps []struct {
		Name    string `json:"name"`
		Stale   bool   `json:"stale"`
		Online  bool   `json:"online"`
		Running *int   `json:"running"`
		Queued  *int   `json:"queued"`
	} `json:"apps"`
}

type buildWorkSample struct {
	Host   string `json:"host"`
	Source string `json:"source"`
	State  string `json:"state"`
	Stale  bool   `json:"stale"`
}

type buildCandidate struct {
	host       buildHost
	activity   int
	cpuPercent float64
}

func selectBuildHost(ctx context.Context, client *ResourcesClient, config buildConfig, job buildJob, onlyHost string, checkedBusy map[string]bool) (buildHost, bool, error) {
	payload, err := client.Do(ctx, http.MethodGet, resourcesPath("state")+"?work_limit=1", nil)
	if err != nil {
		return buildHost{}, false, fmt.Errorf("read build host activity: %w", err)
	}
	var state buildResourcesState
	if err := json.Unmarshal(payload, &state); err != nil {
		return buildHost{}, false, fmt.Errorf("decode build host activity: %w", err)
	}
	candidates, busy, skipped := rankBuildHosts(config.Hosts, state, onlyHost)
	var checks []string
	for _, name := range job.settings.Toolchains {
		checks = append(checks, name, config.Toolchains[name])
	}
	canWait := false
	for _, candidate := range candidates {
		host := candidate.host
		locked, err := probeBuildHost(ctx, config, job, host, checks)
		switch {
		case err != nil:
			delete(checkedBusy, host.ID)
			skipped = append(skipped, fmt.Sprintf("%s: %v", host.ID, err))
		case locked:
			canWait = true
			skipped = append(skipped, host.ID+": another routed build holds the worker lock")
		default:
			return host, false, nil
		}
	}
	// A busy worker only justifies waiting if it can run this lane. Reuse
	// that answer while it is busy and probe again before selecting it.
	if checkedBusy != nil && !canWait {
		for _, host := range busy {
			if !checkedBusy[host.ID] {
				if _, err := probeBuildHost(ctx, config, job, host, checks); err != nil {
					skipped = append(skipped, fmt.Sprintf("%s: %v", host.ID, err))
					continue
				}
				checkedBusy[host.ID] = true
			}
			canWait = true
			break
		}
	}
	if len(skipped) == 0 {
		return buildHost{}, false, fmt.Errorf("no build hosts are enabled")
	}
	return buildHost{}, canWait, fmt.Errorf("no eligible host for %s/%s (%s)", job.repo, job.lane, strings.Join(skipped, "; "))
}

func probeBuildHost(ctx context.Context, config buildConfig, job buildJob, host buildHost, checks []string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(config.ProbeTimeoutSeconds)*time.Second)
	defer cancel()
	output, err := runProbeCommand(probeCtx, job.remoteArgv(host, "probe", checks), nil)
	return strings.TrimSpace(string(output)) == buildLockBusy, err
}

func rankBuildHosts(hosts []buildHost, state buildResourcesState, onlyHost string) ([]buildCandidate, []buildHost, []string) {
	var candidates []buildCandidate
	var busy []buildHost
	var skipped []string
	for _, host := range hosts {
		if !host.Enabled || (onlyHost != "" && host.ID != onlyHost) {
			continue
		}
		index := slices.IndexFunc(state.Hosts, func(sample buildHostSample) bool { return sample.ID == host.ID })
		if index < 0 {
			skipped = append(skipped, host.ID+": missing from Resources")
			continue
		}
		sample := state.Hosts[index]
		reason, occupied := buildHostSkipReason(host, sample, state.Work)
		if reason != "" {
			skipped = append(skipped, host.ID+": "+reason)
			if occupied {
				busy = append(busy, host)
			}
			continue
		}
		activity := 2
		switch sample.Status {
		case "quiet":
			activity = 0
		case "queued":
			activity = 1
		}
		cpuPercent := 101.0
		if sample.Telemetry.CPUPercent != nil {
			cpuPercent = *sample.Telemetry.CPUPercent
		}
		candidates = append(candidates, buildCandidate{host: host, activity: activity, cpuPercent: cpuPercent})
	}
	slices.SortStableFunc(candidates, func(a, b buildCandidate) int {
		return cmp.Or(cmp.Compare(a.activity, b.activity), cmp.Compare(a.cpuPercent, b.cpuPercent))
	})
	return candidates, busy, skipped
}

func buildHostSkipReason(host buildHost, sample buildHostSample, work []buildWorkSample) (string, bool) {
	if sample.Stale || sample.Error != "" || sample.Status == "unknown" || sample.Status == "" {
		return "Resources telemetry is unavailable or stale", false
	}
	for _, entry := range work {
		if entry.Host == host.ID && !entry.Stale && (entry.State == "running" || entry.State == "queued") {
			if entry.Source == buildWorkSource {
				return "a build is already checked in", true
			}
			if host.MaxGPUPercent != nil {
				return "work is checked in on the shared-memory host", true
			}
		}
	}
	if host.MaxGPUPercent == nil {
		return "", false
	}
	for _, app := range sample.Apps {
		if app.Online && !app.Stale && ((app.Running != nil && *app.Running > 0) || (app.Queued != nil && *app.Queued > 0)) {
			return app.Name + " has GPU work running or queued", true
		}
	}
	measured := false
	for _, gpu := range sample.Telemetry.GPUs {
		if gpu.Utilization != nil {
			measured = true
			if *gpu.Utilization > *host.MaxGPUPercent {
				return fmt.Sprintf("GPU utilization %.1f%% exceeds %.1f%%", *gpu.Utilization, *host.MaxGPUPercent), true
			}
		}
	}
	if !measured {
		return "GPU utilization is unavailable", false
	}
	return "", false
}
