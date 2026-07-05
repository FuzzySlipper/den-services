package devserver

import (
	"errors"
	"time"
)

const (
	DefaultBindHost     = "0.0.0.0"
	DefaultProbeHost    = "127.0.0.1"
	PublicHostAuto      = "auto"
	DefaultTarget       = "default"
	SessionSchemaV0     = "den-serve-session/v0"
	DefaultManifestName = ".den-serve.json"
)

type ManagerConfig struct {
	StateDir    string
	SessionRoot string
	BindHost    string
	ProbeHost   string
	PublicHost  string
	PortRange   PortRange
	Timeouts    TimeoutConfig
}

type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type TimeoutConfig struct {
	LockTimeout     time.Duration
	StartupTimeout  time.Duration
	HealthTimeout   time.Duration
	HealthInterval  time.Duration
	ShutdownTimeout time.Duration
}

type ServeManifest struct {
	Project        string
	RepoRoot       string
	ManifestPath   string
	Target         string
	Command        string
	BindHost       string
	ProbeHost      string
	PublicHost     string
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

type UpOptions struct {
	Project            string
	RepoRoot           string
	ManifestPath       string
	PublicHostOverride string
}

type StatusOptions struct {
	Project  string
	RepoRoot string
}

type StopOptions struct {
	Project  string
	RepoRoot string
}

type UpResult struct {
	Session SessionState
	Started bool
	Reused  bool
}

type StopResult struct {
	Session SessionState
	Stopped bool
	Message string
}

type SessionState struct {
	SchemaVersion string       `json:"schema_version"`
	SessionID     string       `json:"session_id"`
	SessionKey    string       `json:"session_key"`
	Project       string       `json:"project"`
	Target        string       `json:"target"`
	RepoRoot      string       `json:"repo_root"`
	ManifestPath  string       `json:"manifest_path"`
	ManifestHash  string       `json:"manifest_hash"`
	Command       string       `json:"command"`
	BindHost      string       `json:"bind_host"`
	ProbeHost     string       `json:"probe_host"`
	PublicHost    string       `json:"public_host,omitempty"`
	Port          int          `json:"port"`
	LocalURL      string       `json:"local_url"`
	LANURL        string       `json:"lan_url,omitempty"`
	HealthURL     string       `json:"health_url"`
	PID           int          `json:"pid,omitempty"`
	Ownership     string       `json:"ownership"`
	ReuseSource   string       `json:"reuse_source"`
	Status        string       `json:"status"`
	Health        HealthResult `json:"health"`
	StartedAt     time.Time    `json:"started_at"`
	LastCheckedAt time.Time    `json:"last_checked_at"`
	StdoutLog     string       `json:"stdout_log"`
	StderrLog     string       `json:"stderr_log"`
	StatePath     string       `json:"state_path"`
	SessionDir    string       `json:"session_dir"`
}

type HealthResult struct {
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	ReadyTextFound bool   `json:"ready_text_found"`
	HeaderMatched  bool   `json:"header_matched"`
	Matched        bool   `json:"matched"`
	Error          string `json:"error,omitempty"`
}

var (
	ErrInvalidConfig   = errors.New("invalid devserver broker config")
	ErrInvalidManifest = errors.New("invalid devserver manifest")
	ErrNoPortAvailable = errors.New("no devserver broker port available")
	ErrSessionNotFound = errors.New("den-serve session not found")
)
