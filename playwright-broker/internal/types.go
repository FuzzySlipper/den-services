package broker

import (
	"errors"
	"time"
)

const (
	DefaultHost           = "127.0.0.1"
	DefaultManifestName   = ".den-playwright.json"
	SchemaVersion         = "den-playwright-run/v0"
	PlaytestSchemaVersion = "den-playwright-playtest/v1"
)

type Config struct {
	StateDir     string
	ArtifactRoot string
	Host         string
	PortRange    PortRange
	Timeouts     TimeoutConfig
	Playtest     PlaytestConfig
}

type PlaytestConfig struct {
	NodeCommand          string
	DriverScript         string
	InputHelper          string
	DriverStartupTimeout time.Duration
	CommandTimeout       time.Duration
}

type PortRange struct {
	Start int
	End   int
}

type TimeoutConfig struct {
	LockTimeout     time.Duration
	StartupTimeout  time.Duration
	HealthTimeout   time.Duration
	HealthInterval  time.Duration
	ShutdownTimeout time.Duration
	RunTimeout      time.Duration
}

type Manifest struct {
	Project  string
	RepoRoot string
	Serve    ServeManifest
	Tests    TestManifest
	Playtest PlaytestManifest
}

type PlaytestManifest struct {
	StartPath   string
	Viewport    Viewport
	Headed      bool
	RecordVideo bool
	Environment map[string]string
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type ServeManifest struct {
	Command        string
	Host           string
	PreferredPort  int
	PortRange      *PortRange
	HealthPath     string
	ReadyText      string
	IdentityHeader string
	ReusePolicy    ReusePolicy
	StartupTimeout time.Duration
	HealthInterval time.Duration
	Environment    map[string]string
}

type TestManifest struct {
	Command        string
	ConfigPath     string
	DefaultArgs    []string
	ArtifactPolicy ArtifactPolicy
	OutputDir      string
	Environment    map[string]string
}

type ReusePolicy string

const (
	ReusePolicyBrokerOwned ReusePolicy = "broker_owned"
	ReusePolicyExplicit    ReusePolicy = "explicit"
	ReusePolicyNever       ReusePolicy = "never"
)

func (p ReusePolicy) IsValid() bool {
	switch p {
	case ReusePolicyBrokerOwned, ReusePolicyExplicit, ReusePolicyNever:
		return true
	}
	return false
}

type ArtifactPolicy string

const (
	ArtifactPolicyStandard ArtifactPolicy = "standard"
	ArtifactPolicyLiveUI   ArtifactPolicy = "live-ui"
)

func (p ArtifactPolicy) RequiresHumanInspection() bool {
	return p == ArtifactPolicyLiveUI
}

type RunOptions struct {
	Project           string
	RepoRoot          string
	ManifestPath      string
	Grep              string
	Headed            bool
	PlaywrightProject string
	Test              string
	ExtraArgs         []string
	DenProjectID      string
	DenTaskID         int64
}

type RunResult struct {
	Evidence Evidence
}

type PlaytestStartOptions struct {
	Project      string
	RepoRoot     string
	ManifestPath string
	Owner        string
	Scenario     string
	Headed       *bool
	Viewport     *Viewport
	RecordVideo  *bool
	DenProjectID string
	DenTaskID    int64
	Metadata     map[string]any
}

type PlaytestSession struct {
	SchemaVersion string    `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	Project       string    `json:"project"`
	RepoRoot      string    `json:"repo_root"`
	Owner         string    `json:"owner,omitempty"`
	Scenario      string    `json:"scenario,omitempty"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at,omitempty"`
	Endpoint      string    `json:"endpoint"`
	DriverPID     int       `json:"driver_pid"`
	ServerPID     int       `json:"server_pid,omitempty"`
	ServerReused  bool      `json:"server_reused"`
	BaseURL       string    `json:"base_url"`
	ArtifactRoot  string    `json:"artifact_root"`
	IndexPath     string    `json:"index_path"`
	StatePath     string    `json:"state_path"`
	Warnings      []string  `json:"warnings,omitempty"`
}

var (
	ErrInvalidManifest = errors.New("invalid playwright broker manifest") //nolint:gochecknoglobals
	ErrNoPortAvailable = errors.New("no broker port available")           //nolint:gochecknoglobals
)
