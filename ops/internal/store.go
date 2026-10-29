package ops

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

// Store owns the Ops SQLite ledger. It is the only code that touches SQL.
type Store struct{ db *sql.DB }

// OpenStore opens (creating if needed) the ledger and applies migrations.
func OpenStore(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("creating ops data directory: %w", err)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening ops database %s: %w", path, err)
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
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `insert into schema_migrations (version, applied_at) values (?, ?)`, version, formatTime(time.Now()))
			return err
		}); err != nil {
			return fmt.Errorf("applying migration %s: %w", version, err)
		}
	}
	return nil
}

func (s *Store) inTx(ctx context.Context, work func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertHost(ctx context.Context, host *Host) error {
	_, err := s.db.ExecContext(ctx, `insert into hosts (host_id, display_name, role, notes, created_at, updated_at)
values (?, ?, ?, ?, ?, ?)
on conflict (host_id) do update set display_name = excluded.display_name, role = excluded.role,
    notes = excluded.notes, updated_at = excluded.updated_at`,
		host.HostID, host.DisplayName, host.Role, host.Notes, formatTime(host.CreatedAt), formatTime(host.UpdatedAt))
	if err != nil {
		return fmt.Errorf("saving host %s: %w", host.HostID, err)
	}
	return nil
}

func (s *Store) ListHosts(ctx context.Context) ([]*Host, error) {
	rows, err := s.db.QueryContext(ctx, `select host_id, display_name, role, notes, created_at, updated_at from hosts order by host_id`)
	if err != nil {
		return nil, fmt.Errorf("listing hosts: %w", err)
	}
	defer rows.Close()
	var hosts []*Host
	for rows.Next() {
		var host Host
		var createdAt, updatedAt string
		if err := rows.Scan(&host.HostID, &host.DisplayName, &host.Role, &host.Notes, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning host: %w", err)
		}
		host.CreatedAt, host.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
		hosts = append(hosts, &host)
	}
	return hosts, rows.Err()
}

const jobColumns = `job_id, host_id, display_name, engine, kind, source, scope, target, schedule_timezone,
    schedule_times, schedule_weekdays, schedule_grace_seconds, heartbeat_stale_seconds, retention,
    recovery_pointer, notes, registered, enabled, created_at, updated_at`

func (s *Store) UpsertJob(ctx context.Context, job *Job) error {
	return s.inTx(ctx, func(tx *sql.Tx) error { return upsertJob(ctx, tx, job) })
}

func upsertJob(ctx context.Context, tx *sql.Tx, job *Job) error {
	var timezone, times, weekdays sql.NullString
	var grace sql.NullInt64
	if job.Schedule != nil {
		timezone = sql.NullString{String: job.Schedule.Timezone(), Valid: true}
		times = sql.NullString{String: strings.Join(job.Schedule.Times(), ","), Valid: true}
		weekdays = sql.NullString{String: strings.Join(job.Schedule.Weekdays(), ","), Valid: true}
		grace = sql.NullInt64{Int64: int64(job.Schedule.Grace() / time.Second), Valid: true}
	}
	_, err := tx.ExecContext(ctx, `insert into jobs (`+jobColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict (job_id) do update set host_id = excluded.host_id, display_name = excluded.display_name,
    engine = excluded.engine, kind = excluded.kind, source = excluded.source, scope = excluded.scope,
    target = excluded.target, schedule_timezone = excluded.schedule_timezone,
    schedule_times = excluded.schedule_times, schedule_weekdays = excluded.schedule_weekdays,
    schedule_grace_seconds = excluded.schedule_grace_seconds,
    heartbeat_stale_seconds = excluded.heartbeat_stale_seconds, retention = excluded.retention,
    recovery_pointer = excluded.recovery_pointer, notes = excluded.notes,
    registered = excluded.registered, enabled = excluded.enabled, updated_at = excluded.updated_at`,
		job.JobID, job.HostID, job.DisplayName, job.Engine, job.Kind, job.Source, job.Scope, job.Target,
		timezone, times, weekdays, grace, int64(job.HeartbeatStaleAfter/time.Second), job.Retention,
		job.RecoveryPointer, job.Notes, boolInt(job.Registered), boolInt(job.Enabled),
		formatTime(job.CreatedAt), formatTime(job.UpdatedAt))
	if err != nil {
		return fmt.Errorf("saving job %s: %w", job.JobID, err)
	}
	return nil
}

func (s *Store) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `select `+jobColumns+` from jobs where job_id = ?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	return job, err
}

func (s *Store) ListJobs(ctx context.Context) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx, `select `+jobColumns+` from jobs order by host_id, job_id`)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}
	defer rows.Close()
	var jobs []*Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(row rowScanner) (*Job, error) {
	var job Job
	var timezone, times, weekdays sql.NullString
	var grace sql.NullInt64
	var staleSeconds, registered, enabled int64
	var createdAt, updatedAt string
	if err := row.Scan(&job.JobID, &job.HostID, &job.DisplayName, &job.Engine, &job.Kind, &job.Source,
		&job.Scope, &job.Target, &timezone, &times, &weekdays, &grace, &staleSeconds, &job.Retention,
		&job.RecoveryPointer, &job.Notes, &registered, &enabled, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if timezone.Valid {
		schedule, err := NewSchedule(timezone.String, splitList(times.String), splitList(weekdays.String), time.Duration(grace.Int64)*time.Second)
		if err != nil {
			return nil, fmt.Errorf("stored schedule for job %s: %w", job.JobID, err)
		}
		job.Schedule = schedule
	}
	job.HeartbeatStaleAfter = time.Duration(staleSeconds) * time.Second
	job.Registered, job.Enabled = registered != 0, enabled != 0
	job.CreatedAt, job.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	return &job, nil
}

const runColumns = `job_id, run_id, host, kind, engine, runner, state, phase, started_at, updated_at,
    finished_at, exit_code, error, reason, warnings_json, notices_json, target, archive_id, archive_name,
    original_bytes, compressed_bytes, deduplicated_bytes, file_count, duration_seconds, complete,
    log_path, manifest_path, run_directory, recovery_pointer, raw_report, received_at, report_count`

func (s *Store) GetRun(ctx context.Context, jobID string, runID string) (*Run, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx, `select `+runColumns+` from runs where job_id = ? and run_id = ?`, jobID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	return run, err
}

// SaveReport stores a run and any verifications it carried in one
// transaction. implicitJob is created only when the job does not exist yet.
func (s *Store) SaveReport(ctx context.Context, implicitJob *Job, run *Run, verifications []*Verification) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if implicitJob != nil {
			if err := upsertJob(ctx, tx, implicitJob); err != nil {
				return err
			}
		}
		if run != nil {
			if err := saveRun(ctx, tx, run); err != nil {
				return err
			}
		}
		for _, verification := range verifications {
			if err := saveVerification(ctx, tx, verification); err != nil {
				return err
			}
		}
		return nil
	})
}

func saveRun(ctx context.Context, tx *sql.Tx, run *Run) error {
	warnings, err := json.Marshal(nonNil(run.Warnings))
	if err != nil {
		return fmt.Errorf("encoding warnings: %w", err)
	}
	notices, err := json.Marshal(nonNil(run.Notices))
	if err != nil {
		return fmt.Errorf("encoding notices: %w", err)
	}
	_, err = tx.ExecContext(ctx, `insert or replace into runs (`+runColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.JobID, run.RunID, run.Host, run.Kind, run.Engine, run.Runner, string(run.State), run.Phase,
		formatTime(run.StartedAt), formatTime(run.UpdatedAt), formatOptionalTime(run.FinishedAt), run.ExitCode,
		run.Error, run.Reason, string(warnings), string(notices), run.Target, run.ArchiveID, run.ArchiveName,
		run.Stats.OriginalBytes, run.Stats.CompressedBytes, run.Stats.DeduplicatedBytes, run.Stats.FileCount,
		run.DurationSeconds, boolInt(run.Complete), run.LogPath, run.ManifestPath, run.RunDirectory,
		run.RecoveryPointer, string(run.RawReport), formatTime(run.ReceivedAt), run.ReportCount)
	if err != nil {
		return fmt.Errorf("saving run %s/%s: %w", run.JobID, run.RunID, err)
	}
	return nil
}

func (s *Store) ListRuns(ctx context.Context, jobID string, limit int) ([]*Run, error) {
	return s.queryRuns(ctx, `select `+runColumns+` from runs where job_id = ? order by started_at desc, run_id desc limit ?`, jobID, limit)
}

// ListProblemRuns returns the newest finished failed or warning runs.
func (s *Store) ListProblemRuns(ctx context.Context, limit int) ([]*Run, error) {
	return s.queryRuns(ctx, `select `+runColumns+` from runs
where state in ('failed', 'warning') and finished_at is not null
order by finished_at desc limit ?`, limit)
}

// LoadJobRuns gathers the runs and checks EvaluateJob needs for one job.
func (s *Store) LoadJobRuns(ctx context.Context, jobID string) (JobRuns, error) {
	var runs JobRuns
	var err error
	queries := []struct {
		target **Run
		where  string
	}{
		{&runs.LastAttempt, ``},
		{&runs.LastStarted, `and state <> 'skipped'`},
		{&runs.LastFinished, `and state <> 'skipped' and finished_at is not null`},
		{&runs.LastSuccess, `and complete = 1 and finished_at is not null`},
		{&runs.LastArchive, `and archive_id <> ''`},
	}
	for _, query := range queries {
		*query.target, err = s.firstRun(ctx, `select `+runColumns+` from runs where job_id = ? `+query.where+`
order by started_at desc, run_id desc limit 1`, jobID)
		if err != nil {
			return runs, err
		}
	}
	if runs.LastVerification, err = s.latestVerification(ctx, jobID, VerificationCheck); err != nil {
		return runs, err
	}
	if runs.LastRestoreTest, err = s.latestVerification(ctx, jobID, VerificationRestoreTest); err != nil {
		return runs, err
	}
	return runs, nil
}

func (s *Store) firstRun(ctx context.Context, query string, args ...any) (*Run, error) {
	runs, err := s.queryRuns(ctx, query, args...)
	if err != nil || len(runs) == 0 {
		return nil, err
	}
	return runs[0], nil
}

func (s *Store) queryRuns(ctx context.Context, query string, args ...any) ([]*Run, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying runs: %w", err)
	}
	defer rows.Close()
	var runs []*Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanRun(row rowScanner) (*Run, error) {
	var run Run
	var state, startedAt, updatedAt, warnings, notices, rawReport, receivedAt string
	var finishedAt sql.NullString
	var exitCode sql.NullInt64
	var complete int64
	err := row.Scan(&run.JobID, &run.RunID, &run.Host, &run.Kind, &run.Engine, &run.Runner, &state, &run.Phase,
		&startedAt, &updatedAt, &finishedAt, &exitCode, &run.Error, &run.Reason, &warnings, &notices,
		&run.Target, &run.ArchiveID, &run.ArchiveName, &run.Stats.OriginalBytes, &run.Stats.CompressedBytes,
		&run.Stats.DeduplicatedBytes, &run.Stats.FileCount, &run.DurationSeconds, &complete, &run.LogPath,
		&run.ManifestPath, &run.RunDirectory, &run.RecoveryPointer, &rawReport, &receivedAt, &run.ReportCount)
	if err != nil {
		return nil, err
	}
	run.State = RunState(state)
	run.StartedAt, run.UpdatedAt, run.ReceivedAt = parseTime(startedAt), parseTime(updatedAt), parseTime(receivedAt)
	if finishedAt.Valid {
		finished := parseTime(finishedAt.String)
		run.FinishedAt = &finished
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		run.ExitCode = &code
	}
	run.Complete = complete != 0
	run.RawReport = []byte(rawReport)
	if err := json.Unmarshal([]byte(warnings), &run.Warnings); err != nil {
		return nil, fmt.Errorf("decoding warnings for run %s: %w", run.RunID, err)
	}
	if err := json.Unmarshal([]byte(notices), &run.Notices); err != nil {
		return nil, fmt.Errorf("decoding notices for run %s: %w", run.RunID, err)
	}
	return &run, nil
}

const verificationColumns = `job_id, verification_id, run_id, kind, check_type, state, at, scope, archive, full_data_read,
    evidence_path, checked_json, raw_report, received_at`

func saveVerification(ctx context.Context, tx *sql.Tx, verification *Verification) error {
	checked, err := json.Marshal(nonNil(verification.Checked))
	if err != nil {
		return fmt.Errorf("encoding checked items: %w", err)
	}
	_, err = tx.ExecContext(ctx, `insert or replace into verifications (`+verificationColumns+`)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		verification.JobID, verification.VerificationID, verification.RunID, string(verification.Kind),
		verification.CheckType, string(verification.State), formatTime(verification.At), verification.Scope, verification.Archive,
		verification.FullDataRead, verification.EvidencePath, string(checked), string(verification.RawReport),
		formatTime(verification.ReceivedAt))
	if err != nil {
		return fmt.Errorf("saving verification %s/%s: %w", verification.JobID, verification.VerificationID, err)
	}
	return nil
}

func (s *Store) GetVerification(ctx context.Context, jobID string, verificationID string) (*Verification, error) {
	verifications, err := s.queryVerifications(ctx, `select `+verificationColumns+` from verifications
where job_id = ? and verification_id = ?`, jobID, verificationID)
	if err != nil {
		return nil, err
	}
	if len(verifications) == 0 {
		return nil, ErrVerificationNotFound
	}
	return verifications[0], nil
}

func (s *Store) ListVerifications(ctx context.Context, jobID string, limit int) ([]*Verification, error) {
	return s.queryVerifications(ctx, `select `+verificationColumns+` from verifications
where job_id = ? order by at desc, verification_id desc limit ?`, jobID, limit)
}

// ListFailedVerifications returns the newest checks or restore tests that did not succeed.
func (s *Store) ListFailedVerifications(ctx context.Context, limit int) ([]*Verification, error) {
	return s.queryVerifications(ctx, `select `+verificationColumns+` from verifications
where state <> 'success' order by at desc limit ?`, limit)
}

func (s *Store) latestVerification(ctx context.Context, jobID string, kind VerificationKind) (*Verification, error) {
	verifications, err := s.queryVerifications(ctx, `select `+verificationColumns+` from verifications
where job_id = ? and kind = ? order by at desc, verification_id desc limit 1`, jobID, string(kind))
	if err != nil || len(verifications) == 0 {
		return nil, err
	}
	return verifications[0], nil
}

func (s *Store) queryVerifications(ctx context.Context, query string, args ...any) ([]*Verification, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying verifications: %w", err)
	}
	defer rows.Close()
	var verifications []*Verification
	for rows.Next() {
		var verification Verification
		var kind, state, at, checked, rawReport, receivedAt string
		var fullDataRead sql.NullBool
		if err := rows.Scan(&verification.JobID, &verification.VerificationID, &verification.RunID, &kind,
			&verification.CheckType, &state, &at, &verification.Scope, &verification.Archive, &fullDataRead, &verification.EvidencePath,
			&checked, &rawReport, &receivedAt); err != nil {
			return nil, fmt.Errorf("scanning verification: %w", err)
		}
		verification.Kind, verification.State = VerificationKind(kind), VerificationState(state)
		verification.At, verification.ReceivedAt = parseTime(at), parseTime(receivedAt)
		if fullDataRead.Valid {
			value := fullDataRead.Bool
			verification.FullDataRead = &value
		}
		verification.RawReport = []byte(rawReport)
		if err := json.Unmarshal([]byte(checked), &verification.Checked); err != nil {
			return nil, fmt.Errorf("decoding checked items for %s: %w", verification.VerificationID, err)
		}
		verifications = append(verifications, &verification)
	}
	return verifications, rows.Err()
}

func formatTime(value time.Time) string { return value.UTC().Format(timeLayout) }

func formatOptionalTime(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*value), Valid: true}
}

// parseTime reads values this store wrote; a malformed row reads as the zero time.
func parseTime(value string) time.Time {
	parsed, err := time.Parse(timeLayout, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
