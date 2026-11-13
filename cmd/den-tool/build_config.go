package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

const defaultBuildQueuePoll = 5 * time.Second

type buildConfig struct {
	WaitForWorker       bool                       `json:"wait_for_worker"`
	ProbeTimeoutSeconds int                        `json:"probe_timeout_seconds"`
	QueuePollSeconds    int                        `json:"queue_poll_seconds"`
	Hosts               []buildHost                `json:"hosts"`
	Toolchains          map[string]string          `json:"toolchains"`
	Repositories        map[string]buildRepository `json:"repositories"`
}

type buildHost struct {
	ID            string   `json:"id"`
	Enabled       bool     `json:"enabled"`
	SSH           []string `json:"ssh"`
	SourceRoot    string   `json:"source_root"`
	BuildRoot     string   `json:"build_root"`
	Jobs          int      `json:"jobs"`
	MaxGPUPercent *float64 `json:"max_gpu_percent"`
}

type buildRepository struct {
	Origin string               `json:"origin"`
	Keep   []string             `json:"keep"`
	Lanes  map[string]buildLane `json:"lanes"`
}

type buildLane struct {
	Command    []string          `json:"command"`
	Toolchains []string          `json:"toolchains"`
	Env        map[string]string `json:"env"`
	MinFreeGiB int               `json:"min_free_gib"`
	OS         string            `json:"os"`
}

func loadBuildConfig(filename string) (buildConfig, error) {
	var config buildConfig
	file, err := os.Open(filename)
	if err != nil {
		return config, fmt.Errorf("open build config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("parse build config %s: %w", filename, err)
	}
	if err := config.validate(); err != nil {
		return config, fmt.Errorf("build config %s: %w", filename, err)
	}
	return config, nil
}

func (c buildConfig) validate() error {
	if c.ProbeTimeoutSeconds <= 0 || len(c.Hosts) == 0 || len(c.Repositories) == 0 {
		return fmt.Errorf("probe_timeout_seconds, hosts and repositories are required")
	}
	if c.QueuePollSeconds < 0 {
		return fmt.Errorf("queue_poll_seconds must be positive when set")
	}
	hosts := make(map[string]bool)
	for _, host := range c.Hosts {
		if host.ID == "" || hosts[host.ID] || len(host.SSH) == 0 || !path.IsAbs(host.BuildRoot) || host.SourceRoot == "" || host.Jobs <= 0 {
			return fmt.Errorf("host %q needs a unique ID, SSH arguments, a source root, an absolute build root and positive jobs", host.ID)
		}
		hosts[host.ID] = true
		if limit := host.MaxGPUPercent; limit != nil && (*limit < 0 || *limit > 100) {
			return fmt.Errorf("host %s: max_gpu_percent must be between 0 and 100", host.ID)
		}
	}
	repoName := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	cachePath := regexp.MustCompile(`^[a-zA-Z0-9._-]+(/[a-zA-Z0-9._-]+)*$`)
	for name, repository := range c.Repositories {
		if !repoName.MatchString(name) || len(repository.Lanes) == 0 {
			return fmt.Errorf("repository %q needs a directory name and at least one headless lane", name)
		}
		// Keep paths are literal checkout-relative cache directories. Wildcards
		// in a git clean exclusion could preserve stale source between runs.
		for _, cache := range repository.Keep {
			if !cachePath.MatchString(cache) || path.Clean(cache) != cache || cache == "." || cache == ".." || strings.HasPrefix(cache, "../") {
				return fmt.Errorf("repository %s: invalid cache directory %q", name, cache)
			}
		}
		for lane, settings := range repository.Lanes {
			if len(settings.Command) == 0 || settings.Command[0] == "" || settings.MinFreeGiB <= 0 {
				return fmt.Errorf("%s/%s needs a command and positive min_free_gib", name, lane)
			}
			for _, toolchain := range settings.Toolchains {
				if c.Toolchains[toolchain] == "" {
					return fmt.Errorf("%s/%s: unknown toolchain %q", name, lane, toolchain)
				}
			}
		}
	}
	return nil
}

func (c buildConfig) queuePollInterval() time.Duration {
	if c.QueuePollSeconds > 0 {
		return time.Duration(c.QueuePollSeconds) * time.Second
	}
	return defaultBuildQueuePoll
}
