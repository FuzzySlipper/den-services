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

func selectBuildHost(ctx context.Context, client *ResourcesClient, config buildConfig, job buildJob, onlyHost string) (buildHost, error) {
	payload, err := client.Do(ctx, http.MethodGet, resourcesPath("state")+"?work_limit=1", nil)
	if err != nil {
		return buildHost{}, fmt.Errorf("read build host activity: %w", err)
	}
	var state buildResourcesState
	if err := json.Unmarshal(payload, &state); err != nil {
		return buildHost{}, fmt.Errorf("decode build host activity: %w", err)
	}
	candidates, skipped := rankBuildHosts(config.Hosts, state, onlyHost)
	var checks []string
	for _, name := range job.settings.Toolchains {
		checks = append(checks, name, config.Toolchains[name])
	}
	for _, candidate := range candidates {
		argv := job.remoteArgv(candidate.host, "probe", checks)
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(config.ProbeTimeoutSeconds)*time.Second)
		_, err := runProbeCommand(probeCtx, argv, nil)
		cancel()
		if err == nil {
			return candidate.host, nil
		}
		skipped = append(skipped, fmt.Sprintf("%s: %v", candidate.host.ID, err))
	}
	if len(skipped) == 0 {
		return buildHost{}, fmt.Errorf("no build hosts are enabled")
	}
	return buildHost{}, fmt.Errorf("no eligible host for %s/%s (%s)", job.repo, job.lane, strings.Join(skipped, "; "))
}

func rankBuildHosts(hosts []buildHost, state buildResourcesState, onlyHost string) ([]buildCandidate, []string) {
	var candidates []buildCandidate
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
		if reason := buildHostSkipReason(host, sample, state.Work); reason != "" {
			skipped = append(skipped, host.ID+": "+reason)
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
	return candidates, skipped
}

func buildHostSkipReason(host buildHost, sample buildHostSample, work []buildWorkSample) string {
	if sample.Stale || sample.Error != "" || sample.Status == "unknown" || sample.Status == "" {
		return "Resources telemetry is unavailable or stale"
	}
	for _, entry := range work {
		if entry.Host == host.ID && !entry.Stale && (entry.State == "running" || entry.State == "queued") {
			if entry.Source == buildWorkSource {
				return "a build is already checked in"
			}
			if host.MaxGPUPercent != nil {
				return "work is checked in on the shared-memory host"
			}
		}
	}
	if host.MaxGPUPercent == nil {
		return ""
	}
	for _, app := range sample.Apps {
		if app.Online && !app.Stale && ((app.Running != nil && *app.Running > 0) || (app.Queued != nil && *app.Queued > 0)) {
			return app.Name + " has GPU work running or queued"
		}
	}
	measured := false
	for _, gpu := range sample.Telemetry.GPUs {
		if gpu.Utilization != nil {
			measured = true
			if *gpu.Utilization > *host.MaxGPUPercent {
				return fmt.Sprintf("GPU utilization %.1f%% exceeds %.1f%%", *gpu.Utilization, *host.MaxGPUPercent)
			}
		}
	}
	if !measured {
		return "GPU utilization is unavailable"
	}
	return ""
}
