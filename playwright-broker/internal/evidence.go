package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Evidence struct {
	SchemaVersion           string             `json:"schema_version"`
	RunID                   string             `json:"run_id"`
	Project                 string             `json:"project"`
	RepoRoot                string             `json:"repo_root"`
	StartedAt               time.Time          `json:"started_at"`
	FinishedAt              time.Time          `json:"finished_at"`
	Status                  string             `json:"status"`
	HumanInspectionRequired bool               `json:"human_inspection_required"`
	Den                     *DenEvidence       `json:"den,omitempty"`
	Server                  ServerEvidence     `json:"server"`
	Playwright              PlaywrightEvidence `json:"playwright"`
	Artifacts               ArtifactEvidence   `json:"artifacts"`
	Warnings                []string           `json:"warnings,omitempty"`
}

type DenEvidence struct {
	ProjectID string `json:"project_id,omitempty"`
	TaskID    int64  `json:"task_id,omitempty"`
}

type ServerEvidence struct {
	Host            string         `json:"host"`
	Port            int            `json:"port"`
	BaseURL         string         `json:"base_url"`
	Command         string         `json:"command"`
	OwnedPID        int            `json:"owned_pid,omitempty"`
	Reused          bool           `json:"reused"`
	ReuseSource     string         `json:"reuse_source,omitempty"`
	Health          HealthEvidence `json:"health"`
	StdoutLog       string         `json:"stdout_log,omitempty"`
	StderrLog       string         `json:"stderr_log,omitempty"`
	StoppedOwnedPID bool           `json:"stopped_owned_pid"`
}

type PlaywrightEvidence struct {
	Command       string        `json:"command"`
	Args          []string      `json:"args"`
	ExitCode      int           `json:"exit_code"`
	Duration      time.Duration `json:"duration_ns"`
	StdoutLog     string        `json:"stdout_log"`
	StderrLog     string        `json:"stderr_log"`
	StdoutExcerpt string        `json:"stdout_excerpt,omitempty"`
	StderrExcerpt string        `json:"stderr_excerpt,omitempty"`
}

type ArtifactEvidence struct {
	Root      string   `json:"root"`
	IndexPath string   `json:"index_path"`
	Files     []string `json:"files"`
}

func writeEvidence(path string, evidence Evidence) error {
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding evidence: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing evidence index: %w", err)
	}
	return nil
}

func listArtifactFiles(root string) ([]string, error) {
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return nil, fmt.Errorf("listing artifact files: %w", err)
	}
	return files, nil
}

func excerpt(data []byte, limit int) string {
	if len(data) <= limit {
		return string(data)
	}
	return string(data[:limit])
}
