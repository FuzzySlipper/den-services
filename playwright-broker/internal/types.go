package broker

import (
	"errors"
	"time"
)

const (
	DefaultHost         = "127.0.0.1"
	DefaultManifestName = ".den-playwright.json"
	SchemaVersion       = "den-playwright-run/v0"
)

type Config struct {
	StateDir     string
	ArtifactRoot string
	Host         string
	PortRange    PortRange
	Timeouts     TimeoutConfig
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

var (
	ErrInvalidManifest = errors.New("invalid playwright broker manifest") //nolint:gochecknoglobals
	ErrNoPortAvailable = errors.New("no broker port available")           //nolint:gochecknoglobals
)
