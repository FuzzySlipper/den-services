# Den Services — Go Codestyle

**Status:** foundation document, revision 3. Adds configuration discipline section (§8).
**Scope:** all Go code in the `den-services` monorepo.
**Enforcement:** code review against this document is mandatory. No code merges without passing codestyle review.

---

## 0. Philosophy: Go like C# — structurally, not ceremonially

Go's defaults are permissive. Agents writing Go without constraint produce code that is functionally correct but architecturally messy: untyped bags, global state, business logic in handlers, and packages that grow until they become god-objects.

This codestyle is deliberately opinionated. It asks agents to write Go with the structural discipline of C#: explicit types, constructor injection, layered architecture, and zero hidden state. The friction of maintaining this discipline is the feature — it prevents the accretion patterns that produced den-channels' current fragility.

### What "Go like C#" means

It means:
- Clear layers (Handler → Service → Store).
- Explicit constructors with validation.
- Typed request/response/domain models.
- Dependency injection by constructor.
- No hidden mutable state.
- No business logic in transport/store code.
- State transitions expressed as named methods.

It does **not** mean:
- AbstractFactoryFactory or giant interface hierarchies.
- Nullable-everything paranoia.
- One-file-per-method ceremony.
- Enterprise abstraction layers with no concrete benefit.

When in doubt, choose the option that is more explicit, more typed, and more boring.

---

## 1. Project structure per service

Every service in the monorepo follows the same internal structure:

```
<domain>/
  config/
    config.example.yaml         # documented example config shipped with the module
  cmd/
    <service>/main.go          # entry point — wiring only, no logic
  internal/
    types.go                   # domain types (structs, enums, errors, constructors)
    config.go                  # typed Config struct loaded from config file
    state.go                   # state machine methods on domain types
    store.go                   # database access (SQL lives here)
    service.go                 # business logic, cross-service coordination
    handler.go                 # HTTP handlers — validate input, shape response
    dto.go                     # request/response DTOs (separate from domain types)
    handler_test.go
    service_test.go
    store_test.go
  go.mod
```

Rules:
- `main.go` contains wiring only: read config, construct dependencies, start server. No business logic.
- `handler.go` validates input, calls the service layer, shapes the HTTP response. No business logic, no SQL.
- `service.go` owns business logic, cross-service coordination, and invariant enforcement. No SQL, no HTTP types.
- `store.go` owns all SQL, including atomic compare-and-swap state transitions. No business logic, no HTTP types.
- `types.go` owns domain types, enums, and constructors. Simple methods (accessors, `IsValid()`) live here.
- `state.go` owns state machine methods on domain types.
- `dto.go` owns request/response DTOs. These are separate from domain types (see §13).
- `config.go` owns the typed config struct and loading logic (see §8).

This is the C# project structure (Controller → Service → Repository) that agents already know. It maps cleanly to Go packages.

### Package sizing

- An `internal/` package should have no more than ~8 non-test `.go` files. Test files (`*_test.go`) do not count toward this limit.
- If a package grows beyond that, it is carrying more than one responsibility. Split it.
- The default answer to "should this be one package or two?" is two — but only if the split follows an authority or invariant boundary, not merely a file-count threshold.

---

## 2. Types and structs

### 2.1 Explicit typed structs everywhere

Every domain concept gets a named struct. No `map[string]interface{}` bags. No `any` where a typed struct communicates intent.

```go
// GOOD
type DeliveryIntent struct {
    id              int64
    targetIdentity  identity.AgentIdentity
    state           IntentState
    idempotencyKey  string
    createdAt       time.Time
    expiresAt       time.Time
    claimedAt       *time.Time
    claimToken      *string
    claimedBy       *identity.AgentIdentity
}

// BAD — untyped bag
type DeliveryIntent map[string]any
```

### 2.2 Private fields for invariant-bearing domain objects

Domain objects that carry invariants (state machines, claim tokens, lifecycle timestamps) use **unexported fields**. External code interacts through constructor functions, accessor methods, and transition methods.

```go
type DeliveryIntent struct {
    id              int64
    state           IntentState
    claimToken      *string
    claimedAt       *time.Time
    claimedBy       *identity.AgentIdentity
    // ... other fields
}

// Accessors
func (i *DeliveryIntent) ID() int64                 { return i.id }
func (i *DeliveryIntent) State() IntentState         { return i.state }
func (i *DeliveryIntent) ClaimToken() *string        { return i.claimToken }

// Transition methods (see §3.2)
func (i *DeliveryIntent) applyClaim(token string, by identity.AgentIdentity, at time.Time) error {
    // ...
}
```

This prevents agents from directly assigning `intent.state = IntentStateClaimed` from outside the package.

**Exception:** simple value objects (config structs, request DTOs, view-model types without invariants) may use exported fields.

### 2.3 Enums as typed constants

Go has no enum keyword. Use typed constants with an underlying string type:

```go
type IntentState string

const (
    IntentStatePending   IntentState = "pending"
    IntentStateClaimed   IntentState = "claimed"
    IntentStateRunning   IntentState = "running"
    IntentStateCompleted IntentState = "completed"
    IntentStateFailed    IntentState = "failed"
    IntentStateExpired   IntentState = "expired"
    IntentStateCancelled IntentState = "cancelled"
)

// IsValid checks whether a state value is recognized.
// Use this at API boundaries to reject unknown values.
func (s IntentState) IsValid() bool {
    switch s {
    case IntentStatePending, IntentStateClaimed, IntentStateRunning,
        IntentStateCompleted, IntentStateFailed, IntentStateExpired,
        IntentStateCancelled:
        return true
    }
    return false
}
```

Rules:
- Enum constants are prefixed with the type name (`IntentState...`) to avoid collisions and aid autocomplete.
- Every enum type has an `IsValid()` method. Use it at API boundaries.
- Never store enum values as table-level `CHECK` constraints in DDL. Validate in the application layer.

### 2.4 Optional fields use pointers

Use `*time.Time`, `*string`, `*int64` for nullable fields. Check `nil` explicitly. Do not use sentinel values like `""` or `0` to mean "absent."

---

## 3. Constructors and rehydration

### 3.1 New-creation constructors

Every exported type that has invariants or requires validation has a constructor function. The constructor enforces all invariants at creation.

```go
func NewDeliveryIntent(target identity.AgentIdentity, idempotencyKey string, ttl time.Duration, clock func() time.Time) (*DeliveryIntent, error) {
    if !target.IsValid() {
        return nil, ErrInvalidIdentity
    }
    if idempotencyKey == "" {
        return nil, ErrMissingIdempotencyKey
    }
    now := clock()
    return &DeliveryIntent{
        id:             generateID(),
        targetIdentity: target,
        state:          IntentStatePending,
        idempotencyKey: idempotencyKey,
        createdAt:      now,
        expiresAt:      now.Add(ttl),
    }, nil
}
```

Rules:
- The constructor generates IDs and timestamps. These are only for **new** objects entering the domain.
- The clock is injected (`func() time.Time`), not hardcoded as `time.Now()`. See §9.3 on clock injection.

### 3.2 Rehydration constructors for DB-loaded records

Loading persisted state from the database uses a separate rehydration path. Rehydration validates persisted state but does **not** generate new IDs, reset timestamps, or reset lifecycle fields.

```go
// rehydrateDeliveryIntent is the package-local rehydration path for store.go.
// It constructs a DeliveryIntent from database column values without generating
// new IDs or timestamps. Unexported — only callable within the package.
func rehydrateDeliveryIntent(
    id int64,
    target identity.AgentIdentity,
    state IntentState,
    idempotencyKey string,
    createdAt, expiresAt time.Time,
    claimedAt *time.Time,
    claimToken *string,
    claimedBy *identity.AgentIdentity,
) (*DeliveryIntent, error) {
    if !state.IsValid() {
        return nil, fmt.Errorf("%w: unknown state %s", ErrCorruptedState, state)
    }
    return &DeliveryIntent{
        id:              id,
        targetIdentity:  target,
        state:           state,
        idempotencyKey:  idempotencyKey,
        createdAt:       createdAt,
        expiresAt:       expiresAt,
        claimedAt:       claimedAt,
        claimToken:      claimToken,
        claimedBy:       claimedBy,
    }, nil
}
```

This avoids the common agent mistake of using the new-creation constructor (which generates IDs/timestamps) when reading from storage.

### 3.3 Transition methods, not direct field assignment

After construction, state transitions go through explicit methods. These methods validate the transition but are **not** the authority for concurrent operations (see §5.3 on atomic store methods).

```go
// applyClaim validates the pending → claimed transition in memory.
// This is used by the store's atomic ClaimPending method to shape the
// resulting value — it is NOT the authority for concurrent claims.
func (i *DeliveryIntent) applyClaim(token string, by identity.AgentIdentity, at time.Time) error {
    if i.state != IntentStatePending {
        return fmt.Errorf("%w: cannot claim intent in state %s", ErrInvalidTransition, i.state)
    }
    i.state = IntentStateClaimed
    i.claimToken = &token
    i.claimedBy = &by
    i.claimedAt = &at
    return nil
}
```

---

## 4. Error handling

### 4.1 Sentinel errors for expected failure modes

Define sentinel errors for every expected failure mode. Callers check with `errors.Is`:

```go
var (
    ErrInvalidTransition    = errors.New("invalid state transition")
    ErrIntentAlreadyClaimed = errors.New("intent already claimed")
    ErrIntentExpired        = errors.New("intent has expired")
    ErrIntentNotFound       = errors.New("intent not found")
    ErrIdempotencyConflict  = errors.New("idempotency key conflict")
    ErrInvalidIdentity      = errors.New("invalid agent identity")
    ErrRuntimeNotAlive      = errors.New("runtime is not alive")
    ErrCorruptedState       = errors.New("corrupted persisted state")
)
```

### 4.2 Typed errors for structured failure context

```go
type InvalidTransitionError struct {
    From IntentState
    To   IntentState
}

func (e *InvalidTransitionError) Error() string {
    return fmt.Sprintf("invalid transition: %s → %s", e.From, e.To)
}
```

### 4.3 Wrap with context, never swallow

```go
// GOOD — wrap with context
if err := rows.Err(); err != nil {
    return fmt.Errorf("scanning delivery intents: %w", err)
}

// BAD — swallowed
if err != nil {
    return errors.New("something went wrong")
}
```

### 4.4 Sentinel-error → HTTP-status mapping

The mapping from sentinel errors to HTTP status codes lives in `shared/api` as a single registry. Handlers call `api.WriteServiceError(w, err)` which consults the registry. Services do not re-derive the mapping.

```go
// In shared/api/error_mapping.go
var statusMap = map[error]int{
    delivery.ErrIntentAlreadyClaimed: http.StatusConflict,
    delivery.ErrIntentExpired:        http.StatusConflict,
    delivery.ErrIntentNotFound:       http.StatusNotFound,
    delivery.ErrRuntimeNotAlive:      http.StatusConflict,
    delivery.ErrInvalidIdentity:      http.StatusBadRequest,
    // ...
}
```

### 4.5 No `panic` in service code

Panics are for programmer errors in `main()` initialization (config missing, can't connect to DB). They are never for expected runtime failures. Use error returns.

---

## 5. Handler → Service → Store layering

### 5.1 Handlers: validate, delegate, shape

```go
func (h *ClaimHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    intentID, err := parseIntentID(r)
    if err != nil {
        api.WriteError(w, http.StatusBadRequest, "invalid intent id")
        return
    }

    var req ClaimRequest
    if err := api.DecodeJSON(r, &req); err != nil {
        api.WriteError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    if err := req.Validate(); err != nil {
        api.WriteError(w, http.StatusBadRequest, err.Error())
        return
    }

    intent, err := h.service.Claim(r.Context(), intentID, req.ClaimToken, req.ClaimedBy)
    if err != nil {
        api.WriteServiceError(w, err) // consults shared/api status registry
        return
    }

    api.WriteJSON(w, http.StatusOK, toClaimResponse(intent))
}
```

Rules:
- Handlers do not contain business logic.
- Handlers do not contain SQL.
- Handlers do not return domain types directly — they return response DTOs (see §13).
- Handlers validate input at the HTTP boundary (format, presence, basic range checks).
- Handlers map errors via `api.WriteServiceError`, which consults the shared status registry.
- Handlers should be short orchestration functions. If a handler exceeds ~40 lines, extract helpers.

### 5.2 Service layer: invariants and cross-service coordination

The service layer coordinates domain logic, cross-service calls, and invariant enforcement. **For state transitions that can be contested by multiple runtimes (claims, completions), the service delegates to atomic store methods — it does not do read-modify-write.**

```go
type ClaimService struct {
    store   IntentStore
    runtime RuntimeChecker
    clock   func() time.Time
}

func NewClaimService(store IntentStore, runtime RuntimeChecker, clock func() time.Time) *ClaimService {
    return &ClaimService{store: store, runtime: runtime, clock: clock}
}

func (s *ClaimService) Claim(ctx context.Context, intentID int64, token string, claimedBy identity.AgentIdentity) (*DeliveryIntent, error) {
    // 1. Gate liveness BEFORE claiming — don't claim then discover the runtime is dead
    alive, err := s.runtime.IsAlive(ctx, claimedBy.InstanceID())
    if err != nil {
        return nil, fmt.Errorf("checking runtime liveness: %w", err)
    }
    if !alive {
        _ = s.store.ExpireIfPending(ctx, intentID, "runtime_not_alive", s.clock())
        return nil, ErrRuntimeNotAlive
    }

    // 2. Atomic claim — the store's ClaimPending does a conditional UPDATE
    //    WHERE state = 'pending' RETURNING *. The database arbitrates the race.
    intent, err := s.store.ClaimPending(ctx, intentID, token, claimedBy, s.clock())
    if err != nil {
        return nil, err
    }

    return intent, nil
}
```

Rules:
- Service methods take `context.Context` as the first argument.
- Service methods return domain types, not HTTP types.
- Service methods enforce invariants and coordinate cross-service calls.
- **For contested state transitions, the service delegates to atomic store methods.** No read-modify-write.
- Service methods should express one use case. If a method exceeds ~60 lines, extract helpers.
- Nested conditionals over ~2 levels should become guard clauses or helper methods.

### 5.3 Store layer: SQL and atomic state transitions

All SQL lives in store files. **Contested state transitions are atomic store methods with compare-and-swap SQL.**

```go
func (s *intentStore) ClaimPending(ctx context.Context, intentID int64, token string, claimedBy identity.AgentIdentity, at time.Time) (*DeliveryIntent, error) {
    row := s.db.QueryRow(ctx, claimPendingSQL, token, claimedBy, at, intentID)
    intent, err := scanIntent(row)
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, ErrIntentAlreadyClaimed
    }
    if err != nil {
        return nil, fmt.Errorf("claiming intent %d: %w", intentID, err)
    }
    return intent, nil
}

const claimPendingSQL = `
    UPDATE den_delivery.delivery_intents
    SET state = 'claimed', claim_token = $1, claimed_by = $2, claimed_at = $3
    WHERE id = $4 AND state = 'pending'
    RETURNING id, target_identity, state, idempotency_key, created_at, expires_at,
              claimed_at, claim_token, claimed_by, completed_at`

func (s *intentStore) ExpireIfPending(ctx context.Context, intentID int64, reason string, at time.Time) error {
    tag, err := s.db.Exec(ctx, expirePendingSQL, reason, at, intentID)
    if err != nil {
        return fmt.Errorf("expiring intent %d: %w", intentID, err)
    }
    if tag.RowsAffected() == 0 {
        return nil
    }
    return nil
}

const expirePendingSQL = `
    UPDATE den_delivery.delivery_intents
    SET state = 'expired'
    WHERE id = $1 AND state = 'pending'`
```

### 5.4 Transaction boundaries

- Transactions begin in the store layer or an explicit unit-of-work helper, never in handlers.
- **No cross-service HTTP calls inside an open database transaction.**
- Document transaction isolation expectations in the store file's comments.

---

## 6. Interfaces and dependency injection

### 6.1 Interfaces defined at the consumer, narrow and use-case-shaped

Define interfaces where they are consumed (in the service layer), not where they are implemented:

```go
// In service.go — the CONSUMER defines the interface it needs
type IntentStore interface {
    ClaimPending(ctx context.Context, id int64, token string, by identity.AgentIdentity, at time.Time) (*DeliveryIntent, error)
    ExpireIfPending(ctx context.Context, id int64, reason string, at time.Time) error
    GetByID(ctx context.Context, id int64) (*DeliveryIntent, error)
    Create(ctx context.Context, intent *DeliveryIntent) error
}

type RuntimeChecker interface {
    IsAlive(ctx context.Context, instanceID identity.AgentInstanceID) (bool, error)
}
```

Rules:
- Define an interface only when there is a real second implementation need: tests (mocks), remote client, alternate store, or boundary seam.
- Interfaces should be small and use-case-shaped. Do not create one giant `Store` interface.
- Do not define interfaces merely because "dependency injection."
- Do not create decorative interface hierarchies. No `AbstractRepositoryBase[T]`.

### 6.2 Dependency injection via constructors

All dependencies are passed through constructor functions. No global singletons, no service locators.

```go
// GOOD
func NewClaimService(store IntentStore, runtime RuntimeChecker, clock func() time.Time) *ClaimService {
    return &ClaimService{store: store, runtime: runtime, clock: clock}
}

// BAD — global singleton
var DefaultStore IntentStore
```

### 6.3 Wiring in main.go

All dependencies are wired in `main.go`:

```go
func main() {
    cfg := config.Load()

    db := postgres.MustConnect(cfg.DatabaseURL)
    defer db.Close()

    intentStore := delivery.NewIntentStore(db)
    runtimeClient := runtime.NewClient(cfg.RuntimeServiceURL)

    claimService := delivery.NewClaimService(intentStore, runtimeClient, time.Now)
    claimHandler := delivery.NewClaimHandler(claimService)

    mux := http.NewServeMux()
    mux.HandleFunc("POST /v1/delivery/intents/{id}/claim", claimHandler.ServeHTTP)

    server := &http.Server{Addr: cfg.BindAddr, Handler: mux}
    log.Fatalf("server error: %v", server.ListenAndServe())
}
```

---

## 7. context.Context discipline

- `context.Context` is the first parameter of every function that does I/O (database, HTTP, time).
- Derive contexts from the request context in handlers: `r.Context()`.
- Never store contexts in structs.
- Pass cancellation naturally through the call chain. Do not create new contexts mid-chain unless adding a timeout.
- Use `context.WithTimeout` for cross-service calls with bounded deadlines.

```go
// GOOD — context threaded explicitly
func (s *ClaimService) Claim(ctx context.Context, intentID int64, ...) (*DeliveryIntent, error) {
    alive, err := s.runtime.IsAlive(ctx, claimedBy.InstanceID())
    // ...
}

// BAD — context ignored or created ad hoc
func (s *ClaimService) Claim(intentID int64) (*DeliveryIntent, error) {
    ctx := context.Background()
    // ...
}
```

---

## 8. Configuration discipline — no hardcoding

Hardcoded values are the most common accretion pattern in agent-written services. A URL, port, timeout, or threshold that starts as a sensible default becomes invisible and unchangeable, then multiplies across files until the service cannot be reconfigured without a code deploy.

**The rule: nothing that varies by deployment or environment is hardcoded.** Configuration values live in files, not in code. The file is visible, intentional, and auditable. A human or agent looking at a service should be able to find every tunable value by reading one file.

### What must never be hardcoded

| Category | Examples | Why |
|---|---|---|
| Network addresses | Service URLs, hostnames, ports, loopback bind addresses | Deployment to a different host or port should not require a code change |
| Connection strings | Database URLs, credentials | Secrets; different per environment |
| Timeouts and intervals | HTTP client timeouts, heartbeat intervals, stale/dead thresholds, reaper sweep interval | Tunable per deployment without recompile |
| TTLs and durations | Delivery intent TTL, cache TTL, claim expiry | Operational tuning |
| Feature flags | Whether identity translation is active, whether reaper is enabled | Safe rollout |
| Limits and sizes | Max request body, replay page size, pool size | Resource tuning |
| Environment identifiers | Service name, datacenter, cluster | Deployment identity |
| Auth tokens and secrets | Service tokens, API keys | Never in source code |

### What MAY be hardcoded (constants that define the system, not tune it)

| Category | Examples |
|---|---|
| Named constants | Enum values (`IntentStatePending`), error sentinel strings |
| Column names in SQL | `SELECT id, state FROM...` — these are the schema contract, not a tunable |
| Route path patterns | `/v1/delivery/intents/{id}/claim` — the API surface, not an environment variable |
| Schema names | `den_delivery`, `den_runtime` — architectural constants |
| JSON field names | `json:"claim_token"` — the wire contract |
| Table names | `delivery_intents` — the schema contract |
| Pure mathematical constants | `maxRetries = 3` — if it's always 3 everywhere for correctness, not tuning |

When in doubt: if changing the value would affect behavior differently in test vs production, or might be tuned by an operator, it belongs in config.

### How configuration is structured

Every module has a typed `Config` struct in `internal/config.go`. The struct is loaded from a YAML or JSON config file. The file path is the **only** thing that comes from an environment variable.

```go
// internal/config.go

type Config struct {
    BindAddr       string        `yaml:"bind_addr"`
    DatabaseURL    string        `yaml:"database_url"`
    RuntimeServiceURL string     `yaml:"runtime_service_url"`
    Heartbeat      HeartbeatConfig `yaml:"heartbeat"`
    Reaper         ReaperConfig  `yaml:"reaper"`
    Delivery       DeliveryConfig `yaml:"delivery"`
}

type HeartbeatConfig struct {
    Interval       time.Duration `yaml:"interval"`        // e.g. "30s"
    StaleThreshold time.Duration `yaml:"stale_threshold"`  // e.g. "90s"
    DeadThreshold  time.Duration `yaml:"dead_threshold"`   // e.g. "300s"
}

type ReaperConfig struct {
    SweepInterval    time.Duration `yaml:"sweep_interval"`
    PendingTTL       time.Duration `yaml:"pending_ttl"`
    RunningTTL       time.Duration `yaml:"running_ttl"`
}

type DeliveryConfig struct {
    DefaultTTL       time.Duration `yaml:"default_ttl"`
    MaxTTL           time.Duration `yaml:"max_ttl"`
}
```

```go
// Loading in main.go
func main() {
    configPath := os.Getenv("DELIVERY_CONFIG_PATH")
    if configPath == "" {
        configPath = "config/config.yaml"
    }

    cfg, err := config.Load(configPath)
    if err != nil {
        log.Fatalf("loading config: %v", err)
    }
    // ...
}
```

### Config validation

Config is validated at load time, not lazily at first use:

```go
func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading config file: %w", err)
    }
    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("parsing config: %w", err)
    }
    if err := cfg.validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }
    return &cfg, nil
}

func (c *Config) validate() error {
    if c.BindAddr == "" {
        return errors.New("bind_addr is required")
    }
    if c.DatabaseURL == "" {
        return errors.New("database_url is required")
    }
    if c.Heartbeat.Interval <= 0 {
        return errors.New("heartbeat.interval must be positive")
    }
    // ... validate every loaded value that has a required range
    return nil
}
```

### Example config files

Every module ships a `config/config.example.yaml` with documented defaults:

```yaml
# config/config.example.yaml — Delivery module configuration
# Copy to config/config.yaml and customize for your deployment.

bind_addr: "127.0.0.1:8080"         # HTTP listen address
database_url: "postgres://..."       # Postgres connection (set via env in production)
runtime_service_url: "http://127.0.0.1:8081"  # Runtime module address

heartbeat:
    interval: "30s"                  # How often the runtime sends heartbeats
    stale_threshold: "90s"           # 3 missed heartbeats → stale
    dead_threshold: "300s"           # 10 missed → dead

reaper:
    sweep_interval: "60s"            # How often the reaper runs
    pending_ttl: "5m"               # Unclaimed intents expire after this
    running_ttl: "30m"              # Running intents with stale runtime expire

delivery:
    default_ttl: "5m"               # Default TTL for new intents
    max_ttl: "1h"                   # Maximum TTL a caller can request
```

The example file is the documentation. Every field has a comment explaining what it does. An operator (or agent) never needs to read Go code to understand the configuration surface.

### Environment variables

Environment variables are used only for:

1. **The config file path** (`DELIVERY_CONFIG_PATH`).
2. **Secrets that must not be in config files** (database password, service tokens). The config file references the env var name, not the value:

```yaml
database_url: "${DEN_DELIVERY_DATABASE_URL}"
```

### No scattered env var configs

Do not create a configuration surface where individual settings are pulled from separate environment variables (`DEN_DELIVERY_HEARTBEAT_INTERVAL`, `DEN_DELIVERY_STALE_THRESHOLD`, `DEN_DELIVERY_DEAD_THRESHOLD`...). This is invisible, untyped, undocumented, and impossible to audit. The config file is the single source of truth; env vars are for secrets and the file path only.

---

## 9. Forbidden patterns and clock/time discipline

### 9.1 No `init()` functions

All initialization is explicit in `main()` or constructor functions.

### 9.2 No package-level mutable state

Sentinel error `var` blocks are the only exception — they are immutable.

### 9.3 Injected clock for time-dependent logic

If time affects a test outcome — state transitions, expiry, TTL — inject a clock (`func() time.Time`). The service constructor accepts the clock; tests inject a fixed clock. Domain transition methods accept `time.Time` as a parameter from the clock, not call `time.Now()` internally.

```go
// GOOD — clock injected
func NewClaimService(store IntentStore, runtime RuntimeChecker, clock func() time.Time) *ClaimService {
    return &ClaimService{store: store, runtime: runtime, clock: clock}
}

// In tests:
svc := NewClaimService(mockStore, mockRuntime, fixedClock)

// BAD — hardcoded time in domain method, defeats injected clock
func (i *DeliveryIntent) applyClaim(token string, by identity.AgentIdentity) error {
    i.claimedAt = ptr(time.Now().UTC())
}
```

For handlers and mappers where time does not affect behavior, use `time.Now().UTC()` directly.

### 9.4 No untyped returns

API responses are typed structs. No returning `map[string]any` or `interface{}` from service methods.

### 9.5 No business logic in SQL

SQL reads and writes rows. State transitions, validation, and invariant enforcement happen in Go. **Exception:** compare-and-swap `WHERE` conditions in atomic store methods are the database's concurrency-control mechanism.

### 9.6 No `interface{}` / `any` in domain types

If a field exists in the domain model, it has a concrete type.

### 9.7 No bare `panic()` in service code

Panics are for unrecoverable programmer errors during initialization only.

### 9.8 No `log.Fatal` outside `main.go`

`log.Fatal` calls `os.Exit(1)`, only acceptable in the entry point.

### 9.9 No clever Go

Forbidden without explicit justification:
- Reflection for domain behavior.
- Goroutines launched from handlers without lifecycle ownership.
- Channel-based concurrency in request paths.
- Anonymous functions for business logic.
- Implicit behavior hidden in package-level registration.
- Dynamic JSON blobs that cross service boundaries.
- `map`/`slice` mutation passed across layers without ownership clarity.

### 9.10 No hardcoded configuration values

Everything tunable per deployment goes in a config file. See §8.

### 9.11 No `SELECT *`

Always specify column lists explicitly. See §12.

---

## 10. Domain aggregate style

Invariant-bearing domain types are aggregates.

1. **Aggregates expose behavior methods for state transitions.**
2. **Mutation-sensitive fields are private.**
3. **The in-memory transition methods are NOT the authority for contested transitions.** The store's atomic compare-and-swap SQL is the authority.
4. **Stores rehydrate aggregates through a named validation path.**
5. **Services coordinate aggregates, stores, clocks, and cross-service clients.** They do not manually poke aggregate fields.
6. **Handlers and DTOs never mutate aggregates directly.**

---

## 11. Testing

### 11.1 Test files live next to source

`handler_test.go` next to `handler.go`. `service_test.go` next to `service.go`. `store_test.go` next to `store.go`.

### 11.2 Table-driven tests

```go
func TestApplyClaimTransition(t *testing.T) {
    tests := []struct {
        name      string
        from      IntentState
        wantErr   error
        wantState IntentState
    }{
        {"claim from pending", IntentStatePending, nil, IntentStateClaimed},
        {"claim from claimed", IntentStateClaimed, ErrInvalidTransition, IntentStateClaimed},
        {"claim from completed", IntentStateCompleted, ErrInvalidTransition, IntentStateCompleted},
        {"claim from expired", IntentStateExpired, ErrInvalidTransition, IntentStateExpired},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            intent := newTestIntent(tt.from)
            err := intent.applyClaim("token", testIdentity, testTime)
            if !errors.Is(err, tt.wantErr) {
                t.Errorf("applyClaim() error = %v, want %v", err, tt.wantErr)
            }
            if intent.State() != tt.wantState {
                t.Errorf("applyClaim() state = %s, want %s", intent.State(), tt.wantState)
            }
        })
    }
}
```

### 11.3 Store tests use a real Postgres instance

Mock the store interface in service tests. Store tests hit a real Postgres instance (a test database, not production).

### 11.4 Service tests mock dependencies

Service tests inject mock implementations of store and cross-service interfaces.

### 11.5 Integration tests in the monorepo's integration/ module

End-to-end tests live in `integration/`.

---

## 12. SQL conventions

- All SQL uses lowercase keywords.
- Parameterized placeholders use `$1, $2, ...` (Postgres positional).
- Column names are `snake_case`.
- Table names are `snake_case`, prefixed with the domain if ambiguous (e.g., `delivery_intents`).
- Timestamps are stored as `TIMESTAMPTZ`. All times are UTC.
- Nullable columns use SQL `NULL`, not sentinel values.
- Foreign keys reference the owning schema explicitly.
- Queries specify column lists explicitly. No `SELECT *`.
- **Compare-and-swap transitions use `WHERE state = '<expected>'` and `RETURNING *`.**

---

## 13. DTO and JSON conventions

### 13.1 DTOs separate from domain types

Public API request/response shapes use **separate DTO structs**, not domain types with JSON tags. Domain types contain internal fields (claim tokens, audit metadata, lifecycle fields) that should not leak.

```go
// dto.go — request and response DTOs

type ClaimRequest struct {
    ClaimToken string                 `json:"claim_token"`
    ClaimedBy  identity.AgentIdentity `json:"claimed_by"`
}

type ClaimResponse struct {
    ID      int64                 `json:"id"`
    State   string                `json:"state"`
    Claimed *ClaimResponseClaim   `json:"claimed,omitempty"`
}

type ClaimResponseClaim struct {
    By        identity.AgentIdentity `json:"by"`
    At        time.Time              `json:"at"`
    TokenHash string                 `json:"-"` // never expose full token
}

func toClaimResponse(intent *DeliveryIntent) ClaimResponse { ... }
```

Rules:
- Domain types do not carry JSON tags. They are internal.
- DTOs live in `dto.go`. Conversion functions (`to*Response`, `from*Request`) live alongside them.
- `json:"-"` is a last-resort guardrail, not the primary security model.
- Field names in JSON are `snake_case`.

**Exception:** simple value objects in `shared/` (like `identity.AgentIdentity`) may carry JSON tags.

### 13.2 Response envelopes

Success: return the resource directly.

Error: consistent envelope.

```json
{
    "error": {
        "code": "intent_already_claimed",
        "message": "delivery intent 42 has already been claimed"
    }
}
```

### 13.3 Request validation

Every request type has a `Validate()` method.

---

## 14. Identity model alignment

This document aligns with `architecture-guidelines` §9. Identity uses the canonical types defined in `shared/identity`:

| Level | Type | JSON field | Meaning |
|---|---|---|---|
| Logical | `ProfileIdentity` | `profile` | Agent profile/role |
| Runtime instance | `AgentInstanceID` | `instance_id` | Specific running instance |
| Session | `SessionKey` | `session_key` | Conversation/work session |

```go
// In shared/identity
type AgentIdentity struct {
    Profile    ProfileIdentity  `json:"profile"`
    InstanceID AgentInstanceID  `json:"instance_id"`
    Session    *SessionKey      `json:"session,omitempty"`
}

func (a AgentIdentity) IsValid() bool {
    return a.Profile != "" && a.InstanceID != ""
}

func (a AgentIdentity) InstanceID() AgentInstanceID { return a.InstanceID }
```

Every table, DTO, and code reference uses these exact types. No module invents its own identity column names.

---

## 15. File header and import ordering

### 15.1 No file header comments

Go files do not need license headers or file-level doc comments. The package doc comment goes on one file per package only.

### 15.2 Import ordering

Three groups, separated by blank lines:

```go
import (
    // stdlib
    "context"
    "fmt"

    // external
    "github.com/jackc/pgx/v5"

    // internal
    "den-services/shared/api"
)
```

---

## 16. Logging

- Use the standard library `log/slog`. No other logging library without architecture review.
- Log to stdout/stderr. Journald handles retention.
- Log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`. No `FATAL` outside `main.go`.
- Include request context (request ID, intent ID, identity) in log entries via structured fields.
- Never log secrets, tokens, or full env file contents.
- Shared logging helpers in `shared/` can attach request ID / service / version / caller identity automatically.

---

## 17. Linter and tooling enforcement

Most rules in this document are mechanically lintable. The repo includes a `.golangci.yml` that enforces them at CI / pre-merge time.

### Machine-enforced rules

| Rule | Section | Linter / mechanism |
|---|---|---|
| No `init()` | §9.1 | `gochecknoinits` |
| No package-level mutable state | §9.2 | `gochecknoglobals` (allowlist sentinel-error `var` blocks) |
| No `panic()` in service code, no `log.Fatal` outside main | §9.7, §9.8 | `forbidigo` (pattern rules, scoped to exclude `cmd/`) |
| No `SELECT *`, parameterized queries | §9.11, §12 | `forbidigo` pattern banning `SELECT *` |
| `errors.Is`/`errors.As` for error checks | §4 | `errorlint` |
| `context.Context` threading | §7 | `contextcheck` |
| HTTP body close | — | `bodyclose` |
| SQL rows close | — | `sqlclosecheck` |
| Import ordering (stdlib, external, internal) | §15.2 | `gofumpt` + `goimports` |
| General formatting | — | `gofmt` / `gofumpt` |
| `go vet` | — | `go vet ./...` |

### Review-only rules (not yet machine-enforced)

| Rule | Section | Why review-only |
|---|---|---|
| Handler → Service → Store layering | §5 | No linter detects "business logic in handler" |
| DTO/domain separation | §13 | Structural convention, not syntactic |
| No hardcoded configuration values | §8 | Judgment call on what is a constant vs config |
| Private fields on invariant-bearing types | §2.2 | Convention, not syntax |
| Atomic store methods for contested transitions | §5.3 | Pattern, not syntax |
| Method size limits | §5 | No reliable complexity linter for Go methods |
| No clever Go patterns | §9.9 | Judgment call |
| Clock injection for time-dependent logic | §9.3 | Convention |
| Narrow interfaces | §6.1 | Design judgment |
| Transaction boundary discipline | §5.4 | Cross-cutting pattern |

The goal is to maximize the first table and minimize the second.

---

## 18. Naming conventions

| Concept | Convention | Example |
|---|---|---|
| Domain type | PascalCase noun | `DeliveryIntent` |
| Enum type | PascalCase noun + `State`/`Kind`/`Status` suffix | `IntentState` |
| Enum constant | TypeName + value | `IntentStatePending` |
| Sentinel error | `Err` + description | `ErrIntentExpired` |
| Typed error struct | PascalCase + `Error` | `InvalidTransitionError` |
| New-creation constructor | `New` + type name | `NewDeliveryIntent` |
| Rehydration constructor | `rehydrate` + type name (unexported) | `rehydrateDeliveryIntent` |
| Store interface | Domain noun + `Store` | `IntentStore` |
| Service struct | Domain noun + `Service` | `ClaimService` |
| Handler struct | Domain noun + `Handler` | `ClaimHandler` |
| Config struct | Type name + `Config` | `DeliveryConfig` |
| Atomic store method | Action + precondition | `ClaimPending`, `ExpireIfPending` |
| Package name | lowercase, single word | `delivery`, `runtime`, `observation` |
| File name | lowercase, matches content | `store.go`, `handler.go`, `config.go` |

---

## 19. Review checklist

Before code is ready to merge, verify:

- [ ] No `init()` functions. (machine-enforced)
- [ ] No package-level mutable state. (machine-enforced)
- [ ] All domain types are explicit structs, not `map[string]any`.
- [ ] Invariant-bearing domain types use private fields with accessors.
- [ ] All exported types with invariants have constructors.
- [ ] New-creation constructor used for fresh objects; rehydration constructor used for DB-loaded records.
- [ ] State transitions are methods with validation, not direct field assignment.
- [ ] Contested state transitions use atomic store methods (compare-and-swap SQL).
- [ ] No read-modify-write for contested state (no TOCTOU races).
- [ ] Sentinel errors defined for expected failure modes. (machine-enforced: `errorlint`)
- [ ] `context.Context` is the first parameter of all I/O functions. (machine-enforced: `contextcheck`)
- [ ] Handler → Service → Store layering maintained. No SQL in handlers/services. No business logic in stores.
- [ ] Interfaces defined at the consumer, narrow and use-case-shaped.
- [ ] All dependencies injected via constructors. No globals.
- [ ] No `panic()` in service code. (machine-enforced)
- [ ] Clock injected for time-dependent logic.
- [ ] **No hardcoded configuration values.** Every tunable is in a config file with defaults documented in `config.example.yaml`. No scattered env var configs.
- [ ] Config file validated at load time.
- [ ] Table-driven tests for state transitions and edge cases.
- [ ] Store tests hit a real Postgres instance.
- [ ] DTOs separate from domain types. Secrets never appear in DTOs.
- [ ] SQL uses parameterized queries. No `SELECT *`. (machine-enforced)
- [ ] Import ordering: stdlib, external, internal. (machine-enforced)
- [ ] `log/slog` used for all logging.
- [ ] No cross-service HTTP calls inside an open DB transaction.
- [ ] Service respects the three-lane invariant (see architecture guidelines).
- [ ] Service does not write to a schema it doesn't own.
- [ ] Haunt-regression tests pass for this module's lane.
