package visualcontract

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ArtifactReferenceContract = "reference.contract.json"
	ArtifactCandidateContract = "candidate.contract.json"
	ArtifactReport            = "report.json"
	ArtifactReferenceOverlay  = "reference.overlay.svg"
	ArtifactCandidateOverlay  = "candidate.overlay.svg"
	ArtifactDiffOverlay       = "diff.overlay.svg"
)

type ArtifactStore interface {
	CreateRun(ctx context.Context, artifacts RunArtifacts) (*VisualContractRun, error)
	GetRun(ctx context.Context, runID string) (*VisualContractRun, error)
	GetArtifact(ctx context.Context, runID string, name string) (*StoredArtifact, error)
}

type RunArtifacts struct {
	RunID             string
	ReferenceContract []byte
	CandidateContract []byte
	Report            []byte
	ReferenceOverlay  []byte
	CandidateOverlay  []byte
	DiffOverlay       []byte
}

type VisualContractRun struct {
	RunID     string            `json:"run_id"`
	CreatedAt time.Time         `json:"created_at"`
	Artifacts map[string]string `json:"artifacts"`
	Names     []string          `json:"names"`
}

type StoredArtifact struct {
	Name        string
	ContentType string
	Body        []byte
}

type FileArtifactStore struct {
	root string
}

func NewFileArtifactStore(root string) *FileArtifactStore {
	return &FileArtifactStore{root: root}
}

func (s *FileArtifactStore) CreateRun(_ context.Context, artifacts RunArtifacts) (*VisualContractRun, error) {
	if strings.TrimSpace(s.root) == "" {
		return nil, invalidRequest("artifact storage path is required")
	}
	runID := artifacts.RunID
	if runID == "" {
		generated, err := newRunID()
		if err != nil {
			return nil, err
		}
		runID = generated
	}
	if !validRunID(runID) {
		return nil, invalidRequest("invalid artifact run id")
	}
	createdAt := time.Now().UTC()
	runDir := filepath.Join(s.root, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating artifact run directory: %w", err)
	}
	files := map[string][]byte{
		ArtifactReferenceContract: artifacts.ReferenceContract,
		ArtifactCandidateContract: artifacts.CandidateContract,
		ArtifactReport:            artifacts.Report,
		ArtifactReferenceOverlay:  artifacts.ReferenceOverlay,
		ArtifactCandidateOverlay:  artifacts.CandidateOverlay,
		ArtifactDiffOverlay:       artifacts.DiffOverlay,
	}
	names := make([]string, 0, len(files))
	for name, body := range files {
		if len(body) == 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(runDir, name), body, 0o644); err != nil {
			return nil, fmt.Errorf("writing artifact %s: %w", name, err)
		}
		names = append(names, name)
	}
	run := &VisualContractRun{
		RunID:     runID,
		CreatedAt: createdAt,
		Artifacts: map[string]string{},
		Names:     names,
	}
	metadata, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding artifact metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "run.json"), metadata, 0o644); err != nil {
		return nil, fmt.Errorf("writing artifact metadata: %w", err)
	}
	return run, nil
}

func (s *FileArtifactStore) GetRun(_ context.Context, runID string) (*VisualContractRun, error) {
	if !validRunID(runID) {
		return nil, notFound("artifact run not found")
	}
	data, err := os.ReadFile(filepath.Join(s.root, runID, "run.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, notFound("artifact run not found")
	}
	if err != nil {
		return nil, fmt.Errorf("reading artifact metadata: %w", err)
	}
	var run VisualContractRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("decoding artifact metadata: %w", err)
	}
	return &run, nil
}

func (s *FileArtifactStore) GetArtifact(_ context.Context, runID string, name string) (*StoredArtifact, error) {
	if !validRunID(runID) || !validArtifactName(name) {
		return nil, notFound("artifact not found")
	}
	body, err := os.ReadFile(filepath.Join(s.root, runID, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, notFound("artifact not found")
	}
	if err != nil {
		return nil, fmt.Errorf("reading artifact: %w", err)
	}
	return &StoredArtifact{
		Name:        name,
		ContentType: artifactContentType(name),
		Body:        body,
	}, nil
}

func newRunID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generating run id: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func validRunID(runID string) bool {
	if len(runID) != 24 {
		return false
	}
	for _, r := range runID {
		if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func validArtifactName(name string) bool {
	switch name {
	case ArtifactReferenceContract, ArtifactCandidateContract, ArtifactReport,
		ArtifactReferenceOverlay, ArtifactCandidateOverlay, ArtifactDiffOverlay:
		return true
	}
	return false
}

func artifactContentType(name string) string {
	if strings.HasSuffix(name, ".svg") {
		return "image/svg+xml"
	}
	return "application/json"
}

func notFound(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrNotFound, message), "not_found", 404)
}

func artifactURL(baseURL string, runID string, name string) string {
	return strings.TrimRight(baseURL, "/") + "/" + runID + "/artifacts/" + name
}
