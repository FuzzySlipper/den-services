package resources

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Registers the pure-Go "sqlite" database/sql driver.
)

// timeLayout has a fixed width so stored text sorts in time order.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store owns the Resources SQLite database. It is the only code that touches
// SQL; the rules about what may change live in Service.
type Store struct{ db *sql.DB }

// Tx is one transaction. Service runs each request's reads and writes in a
// single Tx so a rule check and its write cannot interleave with another.
type Tx struct {
	ctx context.Context
	tx  *sql.Tx
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating resources data directory: %w", err)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening resources database %s: %w", path, err)
	}
	// One writer keeps SQLite simple; reads are small and fast.
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `create table if not exists schema_migrations (version text primary key, applied_at text not null) strict`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("listing migrations: %w", err)
	}
	slices.Sort(names)
	for _, name := range names {
		version := strings.TrimSuffix(filepath.Base(name), ".sql")
		var applied int
		if err := s.db.QueryRowContext(ctx, `select count(1) from schema_migrations where version = ?`, version).Scan(&applied); err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}
		if err := s.InTx(ctx, func(tx *Tx) error {
			if _, err := tx.tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.tx.ExecContext(ctx, `insert into schema_migrations (version, applied_at) values (?, ?)`, version, formatTime(time.Now()))
			return err
		}); err != nil {
			return fmt.Errorf("applying migration %s: %w", version, err)
		}
	}
	return nil
}

// InTx runs work in one transaction, committing only if it returns nil.
func (s *Store) InTx(ctx context.Context, work func(tx *Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := work(&Tx{ctx: ctx, tx: sqlTx}); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("committing: %w", err)
	}
	return nil
}

// Settings

func (t *Tx) Setting(key string) (string, bool, error) {
	var value string
	err := t.tx.QueryRowContext(t.ctx, `select value from settings where key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading setting %s: %w", key, err)
	}
	return value, true, nil
}

func (t *Tx) PutSetting(key string, value string) error {
	_, err := t.tx.ExecContext(t.ctx, `insert into settings (key, value) values (?, ?)
on conflict (key) do update set value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("saving setting %s: %w", key, err)
	}
	return nil
}

// Hosts

const hostColumns = `host_id, name, role, access, note, position, telemetry, error, attempted_at, sampled_at,
    received_at, apps, apps_checked_at, created_at, updated_at`

func (t *Tx) PutHost(host *Host) error {
	apps, err := json.Marshal(nonNil(host.Apps))
	if err != nil {
		return fmt.Errorf("encoding apps for %s: %w", host.ID, err)
	}
	var telemetry sql.NullString
	if len(host.Telemetry) > 0 {
		telemetry = sql.NullString{String: string(host.Telemetry), Valid: true}
	}
	_, err = t.tx.ExecContext(t.ctx, `insert into hosts (`+hostColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict (host_id) do update set name = excluded.name, role = excluded.role, access = excluded.access,
    note = excluded.note, position = excluded.position, telemetry = excluded.telemetry, error = excluded.error,
    attempted_at = excluded.attempted_at, sampled_at = excluded.sampled_at, received_at = excluded.received_at,
    apps = excluded.apps, apps_checked_at = excluded.apps_checked_at, updated_at = excluded.updated_at`,
		host.ID, host.Name, host.Role, host.Access, host.Note, host.Position, telemetry, optionalString(host.Error),
		formatOptionalTime(host.AttemptedAt), formatOptionalTime(host.SampledAt), formatOptionalTime(host.ReceivedAt),
		string(apps), formatOptionalTime(host.AppsCheckedAt), formatTime(host.CreatedAt), formatTime(host.UpdatedAt))
	if err != nil {
		return fmt.Errorf("saving host %s: %w", host.ID, err)
	}
	return nil
}

// Host returns the host, or nil when it is not registered.
func (t *Tx) Host(id string) (*Host, error) {
	hosts, err := t.queryHosts(`select `+hostColumns+` from hosts where host_id = ?`, id)
	if err != nil || len(hosts) == 0 {
		return nil, err
	}
	return hosts[0], nil
}

func (t *Tx) Hosts() ([]*Host, error) {
	return t.queryHosts(`select ` + hostColumns + ` from hosts order by position, host_id`)
}

func (t *Tx) queryHosts(query string, args ...any) ([]*Host, error) {
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading hosts: %w", err)
	}
	defer rows.Close()
	var hosts []*Host
	for rows.Next() {
		var host Host
		var telemetry, hostError, attemptedAt, sampledAt, receivedAt, appsCheckedAt sql.NullString
		var apps, createdAt, updatedAt string
		if err := rows.Scan(&host.ID, &host.Name, &host.Role, &host.Access, &host.Note, &host.Position, &telemetry,
			&hostError, &attemptedAt, &sampledAt, &receivedAt, &apps, &appsCheckedAt, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning host: %w", err)
		}
		if telemetry.Valid {
			host.Telemetry = json.RawMessage(telemetry.String)
		}
		host.Error = hostError.String
		host.AttemptedAt, host.SampledAt, host.ReceivedAt = parseOptionalTime(attemptedAt), parseOptionalTime(sampledAt), parseOptionalTime(receivedAt)
		host.AppsCheckedAt = parseOptionalTime(appsCheckedAt)
		host.CreatedAt, host.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		if err := json.Unmarshal([]byte(apps), &host.Apps); err != nil {
			return nil, fmt.Errorf("decoding apps for %s: %w", host.ID, err)
		}
		hosts = append(hosts, &host)
	}
	return hosts, rows.Err()
}

// Resources

const resourceColumns = `resource_id, host_id, name, kind, path, probe_path, runtime_path, access, sharing,
    lifecycle, notes, guide, match, updated_at, last_used_at, last_used_by, observed_activity_at, confirmed_at,
    confirmed_by, path_present, path_checked_at`

func (t *Tx) PutResource(r *Resource) error {
	var present sql.NullInt64
	if r.PathPresent != nil {
		present = sql.NullInt64{Int64: boolInt(*r.PathPresent), Valid: true}
	}
	_, err := t.tx.ExecContext(t.ctx, `insert into resources (`+resourceColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict (resource_id) do update set host_id = excluded.host_id, name = excluded.name, kind = excluded.kind,
    path = excluded.path, probe_path = excluded.probe_path, runtime_path = excluded.runtime_path,
    access = excluded.access, sharing = excluded.sharing, lifecycle = excluded.lifecycle, notes = excluded.notes,
    guide = excluded.guide, match = excluded.match, updated_at = excluded.updated_at,
    last_used_at = excluded.last_used_at, last_used_by = excluded.last_used_by,
    observed_activity_at = excluded.observed_activity_at, confirmed_at = excluded.confirmed_at,
    confirmed_by = excluded.confirmed_by, path_present = excluded.path_present,
    path_checked_at = excluded.path_checked_at`,
		r.ID, r.Host, r.Name, r.Kind, r.Path, r.ProbePath, r.RuntimePath, r.Access, r.Sharing, string(r.Lifecycle),
		r.Notes, r.Guide, r.Match, formatTime(r.UpdatedAt), formatOptionalTime(r.LastUsedAt), r.LastUsedBy,
		formatOptionalTime(r.ObservedActivityAt), formatOptionalTime(r.ConfirmedAt), r.ConfirmedBy, present,
		formatOptionalTime(r.PathCheckedAt))
	if err != nil {
		return fmt.Errorf("saving resource %s: %w", r.ID, err)
	}
	return nil
}

// Resource returns the resource, or nil when there is none.
func (t *Tx) Resource(id string) (*Resource, error) {
	found, err := t.queryResources(`select `+resourceColumns+` from resources where resource_id = ?`, id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// Resources lists the directory; a non-empty host narrows it to that host.
func (t *Tx) Resources(host string) ([]*Resource, error) {
	if host != "" {
		return t.queryResources(`select `+resourceColumns+` from resources where host_id = ? order by resource_id`, host)
	}
	return t.queryResources(`select ` + resourceColumns + ` from resources order by host_id, resource_id`)
}

func (t *Tx) queryResources(query string, args ...any) ([]*Resource, error) {
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading resources: %w", err)
	}
	defer rows.Close()
	var found []*Resource
	for rows.Next() {
		var r Resource
		var lifecycle, updatedAt string
		var lastUsedAt, observedAt, confirmedAt, checkedAt sql.NullString
		var present sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Host, &r.Name, &r.Kind, &r.Path, &r.ProbePath, &r.RuntimePath, &r.Access,
			&r.Sharing, &lifecycle, &r.Notes, &r.Guide, &r.Match, &updatedAt, &lastUsedAt, &r.LastUsedBy, &observedAt,
			&confirmedAt, &r.ConfirmedBy, &present, &checkedAt); err != nil {
			return nil, fmt.Errorf("scanning resource: %w", err)
		}
		r.Lifecycle, r.UpdatedAt = Lifecycle(lifecycle), parseTime(updatedAt)
		r.LastUsedAt, r.ObservedActivityAt = parseOptionalTime(lastUsedAt), parseOptionalTime(observedAt)
		r.ConfirmedAt, r.PathCheckedAt = parseOptionalTime(confirmedAt), parseOptionalTime(checkedAt)
		if present.Valid {
			value := present.Int64 != 0
			r.PathPresent = &value
		}
		found = append(found, &r)
	}
	return found, rows.Err()
}

// Work

const workColumns = `work_id, host_id, source, external_id, title, owner, project, resources, state, note, url,
    ttl_seconds, created_at, started_at, updated_at, expires_at, ended_at`

func (t *Tx) PutWork(w *Work) error {
	resources, err := json.Marshal(nonNil(w.Resources))
	if err != nil {
		return fmt.Errorf("encoding work resources: %w", err)
	}
	var external sql.NullString
	if w.ExternalID != nil {
		external = sql.NullString{String: *w.ExternalID, Valid: true}
	}
	_, err = t.tx.ExecContext(t.ctx, `insert into work (`+workColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict (work_id) do update set title = excluded.title, owner = excluded.owner, project = excluded.project,
    resources = excluded.resources, state = excluded.state, note = excluded.note, url = excluded.url,
    ttl_seconds = excluded.ttl_seconds, started_at = excluded.started_at, updated_at = excluded.updated_at,
    expires_at = excluded.expires_at, ended_at = excluded.ended_at`,
		w.ID, w.Host, w.Source, external, w.Title, w.Owner, w.Project, string(resources), string(w.State), w.Note,
		w.URL, w.TTL.Seconds(), formatTime(w.CreatedAt), formatOptionalTime(w.StartedAt), formatTime(w.UpdatedAt),
		formatTime(w.ExpiresAt), formatOptionalTime(w.EndedAt))
	if err != nil {
		return fmt.Errorf("saving work %s: %w", w.ID, err)
	}
	return nil
}

// Work returns the row, or nil when there is none.
func (t *Tx) Work(id string) (*Work, error) {
	found, err := t.queryWork(`select `+workColumns+` from work where work_id = ?`, id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// WorkByExternalID finds the row a reporter's own job ID names, or nil.
func (t *Tx) WorkByExternalID(source string, host string, externalID string) (*Work, error) {
	found, err := t.queryWork(`select `+workColumns+` from work where source = ? and host_id = ? and external_id = ?`,
		source, host, externalID)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// ListWork returns rows newest report first. Without ActiveOnly, active rows
// are always included and the limit applies to the finished ones.
func (t *Tx) ListWork(filter WorkFilter) ([]*Work, error) {
	where, args := []string{"1 = 1"}, []any{}
	if filter.Source != "" {
		where, args = append(where, "source = ?"), append(args, filter.Source)
	}
	if filter.Host != "" {
		where, args = append(where, "host_id = ?"), append(args, filter.Host)
	}
	clause := strings.Join(where, " and ")
	active, err := t.queryWork(`select `+workColumns+` from work where `+clause+
		` and state in ('queued', 'running') order by updated_at desc`, args...)
	if err != nil || filter.ActiveOnly {
		return active, err
	}
	finished, err := t.queryWork(`select `+workColumns+` from work where `+clause+
		` and state not in ('queued', 'running') order by updated_at desc limit ?`, append(args, filter.Limit)...)
	if err != nil {
		return nil, err
	}
	all := append(active, finished...)
	slices.SortStableFunc(all, func(a, b *Work) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return all, nil
}

func (t *Tx) queryWork(query string, args ...any) ([]*Work, error) {
	rows, err := t.tx.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading work: %w", err)
	}
	defer rows.Close()
	var found []*Work
	for rows.Next() {
		var w Work
		var external, startedAt, endedAt sql.NullString
		var resources, state, createdAt, updatedAt, expiresAt string
		var ttl float64
		if err := rows.Scan(&w.ID, &w.Host, &w.Source, &external, &w.Title, &w.Owner, &w.Project, &resources, &state,
			&w.Note, &w.URL, &ttl, &createdAt, &startedAt, &updatedAt, &expiresAt, &endedAt); err != nil {
			return nil, fmt.Errorf("scanning work: %w", err)
		}
		if external.Valid {
			w.ExternalID = &external.String
		}
		if err := json.Unmarshal([]byte(resources), &w.Resources); err != nil {
			return nil, fmt.Errorf("decoding resources of work %s: %w", w.ID, err)
		}
		w.State, w.TTL = WorkState(state), time.Duration(ttl*float64(time.Second))
		w.CreatedAt, w.UpdatedAt, w.ExpiresAt = parseTime(createdAt), parseTime(updatedAt), parseTime(expiresAt)
		w.StartedAt, w.EndedAt = parseOptionalTime(startedAt), parseOptionalTime(endedAt)
		found = append(found, &w)
	}
	return found, rows.Err()
}

func formatTime(value time.Time) string { return value.UTC().Format(timeLayout) }

func formatOptionalTime(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*value), Valid: true}
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(timeLayout, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := parseTime(value.String)
	return &parsed
}

func optionalString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
