package resources

import (
	"encoding/json"
	"strings"
	"time"
)

// probeSnapshot is the part of a probe's telemetry the service acts on. The
// whole snapshot is stored as sent.
type probeSnapshot struct {
	SampledAt *time.Time
	Paths     map[string]bool
	processes []string // command and cwd, for resource match strings
}

func (p probeSnapshot) matches(needle string) bool {
	for _, process := range p.processes {
		if strings.Contains(process, needle) {
			return true
		}
	}
	return false
}

// telemetryMetrics names the numeric fields that must be finite numbers or
// null. A reporter's malformed metric is rejected with a specific correction
// instead of replacing usable data or breaking the board.
var telemetryMetrics = map[string][]string{ //nolint:gochecknoglobals
	"":           {"sampled_at", "cpu_count", "cpu_percent"},
	"memory":     {"total_bytes", "available_bytes", "swap_total_bytes", "swap_free_bytes"},
	"gpus":       {"utilization_percent", "memory_total_bytes", "memory_used_bytes", "gtt_used_bytes"},
	"processes":  {"cpu_percent", "rss_bytes"},
	"disks":      {"total_bytes", "free_bytes"},
	"containers": {},
}

func parseTelemetry(raw json.RawMessage) (probeSnapshot, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return probeSnapshot{}, validationFailed("telemetry must be a JSON object")
	}
	for section, fields := range telemetryMetrics {
		switch section {
		case "":
			if err := checkMetrics(top, fields); err != nil {
				return probeSnapshot{}, err
			}
		case "memory":
			if value, ok := top[section]; ok && value != nil {
				row, isObject := value.(map[string]any)
				if !isObject {
					return probeSnapshot{}, validationFailed("telemetry memory must be a JSON object")
				}
				if err := checkMetrics(row, fields); err != nil {
					return probeSnapshot{}, err
				}
			}
		default:
			value, ok := top[section]
			if !ok || value == nil {
				continue
			}
			rows, isArray := value.([]any)
			if !isArray {
				return probeSnapshot{}, validationFailed("telemetry %s must be a JSON array", section)
			}
			for _, item := range rows {
				row, isObject := item.(map[string]any)
				if !isObject {
					return probeSnapshot{}, validationFailed("telemetry %s entries must be JSON objects", section)
				}
				if err := checkMetrics(row, fields); err != nil {
					return probeSnapshot{}, err
				}
			}
		}
	}
	snapshot := probeSnapshot{Paths: map[string]bool{}}
	if seconds, ok := top["sampled_at"].(float64); ok {
		at := unixTime(seconds)
		snapshot.SampledAt = &at
	}
	if paths, ok := top["paths"].(map[string]any); ok {
		for path, present := range paths {
			if value, isBool := present.(bool); isBool {
				snapshot.Paths[path] = value
			}
		}
	}
	if processes, ok := top["processes"].([]any); ok {
		for _, item := range processes {
			row, _ := item.(map[string]any)
			command, _ := row["command"].(string)
			cwd, _ := row["cwd"].(string)
			snapshot.processes = append(snapshot.processes, command+" "+cwd)
		}
	}
	return snapshot, nil
}

func checkMetrics(row map[string]any, fields []string) error {
	for _, field := range fields {
		value, ok := row[field]
		if !ok || value == nil {
			continue
		}
		if _, isNumber := value.(float64); !isNumber {
			return validationFailed("telemetry %s must be a finite number or null, without units", field)
		}
	}
	return nil
}
