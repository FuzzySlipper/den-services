package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"den-services/shared/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type importStep struct {
	Domain       string
	Source       string
	Target       string
	SQL          string
	SourceCount  string
	TargetCount  string
	SourceKeySQL string
	TargetKeySQL string
	Rerun        string
}

type skippedStep struct {
	Domain    string
	Source    string
	Target    string
	Rationale string
}

type stepReport struct {
	Domain         string
	Source         string
	Target         string
	SourceCount    int64
	TargetCount    int64
	SourceChecksum string
	TargetChecksum string
	Mismatch       string
	Rerun          string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("lifeboat backfill failed: %v", err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("lifeboat-backfill", flag.ContinueOnError)
	databaseURL := flags.String("database-url", databaseURLFromEnv(), "Postgres URL for migration role")
	resetTarget := flags.Bool("reset-target", false, "TRUNCATE successor targets before importing")
	dryRun := flags.Bool("dry-run", false, "Report counts without writing")
	timeout := flags.Duration("timeout", 5*time.Minute, "Backfill timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *databaseURL == "" {
		return postgres.ErrMissingDatabaseURL
	}
	if !*dryRun && !*resetTarget {
		return fmt.Errorf("--reset-target is required for writes; use --dry-run for read-only inspection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: *databaseURL})
	if err != nil {
		return err
	}
	defer pool.Close()

	reports, skipped, err := backfill(ctx, pool, *resetTarget, *dryRun)
	if err != nil {
		return err
	}
	fmt.Println(formatReport(reports, skipped, *dryRun))
	return nil
}

func databaseURLFromEnv() string {
	if value := os.Getenv("DEN_MIGRATION_DATABASE_URL"); value != "" {
		return value
	}
	return os.Getenv("DEN_SERVICES_MIGRATION_DATABASE_URL")
}

func backfill(ctx context.Context, pool *pgxpool.Pool, resetTarget bool, dryRun bool) ([]stepReport, []skippedStep, error) {
	steps := importSteps()
	skipped := skippedSteps()

	if dryRun {
		preflight, err := collectCountsAndChecksums(ctx, pool, steps)
		if err != nil {
			return nil, nil, err
		}
		return preflight, skipped, nil
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("beginning lifeboat backfill: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if resetTarget {
		if _, err := tx.Exec(ctx, truncateTargetsSQL()); err != nil {
			return nil, nil, fmt.Errorf("truncating lifeboat targets: %w", err)
		}
	}
	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.SQL); err != nil {
			return nil, nil, fmt.Errorf("importing %s into %s: %w", step.Source, step.Target, err)
		}
	}
	if _, err := tx.Exec(ctx, resetSequencesSQL()); err != nil {
		return nil, nil, fmt.Errorf("resetting lifeboat identity sequences: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("committing lifeboat backfill: %w", err)
	}

	after, err := collectCountsAndChecksums(ctx, pool, steps)
	if err != nil {
		return nil, nil, err
	}
	return after, skipped, nil
}

func collectCountsAndChecksums(ctx context.Context, pool *pgxpool.Pool, steps []importStep) ([]stepReport, error) {
	reports := make([]stepReport, 0, len(steps))
	for _, step := range steps {
		sourceCount, err := readInt64(ctx, pool, step.SourceCount)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", step.Source, err)
		}
		targetCount, err := readInt64(ctx, pool, step.TargetCount)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", step.Target, err)
		}
		sourceChecksum, err := readChecksum(ctx, pool, step.SourceKeySQL)
		if err != nil {
			return nil, fmt.Errorf("checksumming %s: %w", step.Source, err)
		}
		targetChecksum, err := readChecksum(ctx, pool, step.TargetKeySQL)
		if err != nil {
			return nil, fmt.Errorf("checksumming %s: %w", step.Target, err)
		}
		mismatch := ""
		if sourceCount != targetCount {
			mismatch = "count"
		} else if sourceChecksum != targetChecksum {
			mismatch = "key_checksum"
		}
		reports = append(reports, stepReport{
			Domain:         step.Domain,
			Source:         step.Source,
			Target:         step.Target,
			SourceCount:    sourceCount,
			TargetCount:    targetCount,
			SourceChecksum: sourceChecksum,
			TargetChecksum: targetChecksum,
			Mismatch:       mismatch,
			Rerun:          step.Rerun,
		})
	}
	return reports, nil
}

func readInt64(ctx context.Context, pool *pgxpool.Pool, sql string) (int64, error) {
	var value int64
	if err := pool.QueryRow(ctx, sql).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func readChecksum(ctx context.Context, pool *pgxpool.Pool, sql string) (string, error) {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return "", err
		}
		cells := make([]string, len(values))
		for index, value := range values {
			if value == nil {
				cells[index] = "<null>"
				continue
			}
			cells[index] = fmt.Sprint(value)
		}
		parts = append(parts, strings.Join(cells, "\x1f"))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func formatReport(reports []stepReport, skipped []skippedStep, dryRun bool) string {
	var b strings.Builder
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(&b, "lifeboat backfill report mode=%s\n", mode)
	fmt.Fprintf(&b, "%-10s %-34s %-40s %8s %8s %-12s %-64s\n", "domain", "source", "target", "source", "target", "mismatch", "key_checksum")
	for _, report := range reports {
		checksum := "ok"
		if report.SourceChecksum != report.TargetChecksum {
			checksum = report.SourceChecksum[:12] + "!=" + report.TargetChecksum[:12]
		}
		mismatch := report.Mismatch
		if mismatch == "" {
			mismatch = "-"
		}
		fmt.Fprintf(&b, "%-10s %-34s %-40s %8d %8d %-12s %-64s\n",
			report.Domain, report.Source, report.Target, report.SourceCount, report.TargetCount, mismatch, checksum)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\nskipped domains/tables\n")
		for _, skip := range skipped {
			fmt.Fprintf(&b, "- %s: %s -> %s: %s\n", skip.Domain, skip.Source, skip.Target, skip.Rationale)
		}
	}
	fmt.Fprintf(&b, "\nrerun behavior: destructive recreate with guardrail; writes require --reset-target and truncate only successor target tables.\n")
	return strings.TrimRight(b.String(), "\n")
}

func importSteps() []importStep {
	recreate := "destructive --reset-target"
	return []importStep{
		{
			Domain:       "projects",
			Source:       "den_core.projects",
			Target:       "den_projects.projects",
			SourceCount:  "select count(*) from den_core.projects",
			TargetCount:  "select count(*) from den_projects.projects",
			SourceKeySQL: "select id from den_core.projects order by id",
			TargetKeySQL: "select id from den_projects.projects order by id",
			Rerun:        recreate,
			SQL: `
insert into den_projects.projects(id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at)
select id, name, kind, visibility, owner, root_path, description, settings_json, created_at::timestamptz, updated_at::timestamptz
from den_core.projects
order by id`,
		},
		{
			Domain:       "tasks",
			Source:       "den_core.tasks",
			Target:       "den_tasks.tasks",
			SourceCount:  "select count(*) from den_core.tasks",
			TargetCount:  "select count(*) from den_tasks.tasks",
			SourceKeySQL: "select id from den_core.tasks order by id",
			TargetKeySQL: "select id from den_tasks.tasks order by id",
			Rerun:        recreate,
			SQL: `
insert into den_tasks.tasks(id, project_id, parent_id, title, description, status, priority, assigned_to, tags, blocker_summary, blocker_reason, blocker_attempted_remedies, blocker_suggested_next_step, blocker_requires_human_input, created_at, updated_at)
overriding system value
select id, project_id, parent_id, title, description, status, priority, assigned_to, tags, blocker_summary, blocker_reason, blocker_attempted_remedies, blocker_suggested_next_step, blocker_requires_human_input, created_at::timestamptz, updated_at::timestamptz
from den_core.tasks
order by id`,
		},
		{
			Domain:       "tasks",
			Source:       "den_core.task_dependencies",
			Target:       "den_tasks.task_dependencies",
			SourceCount:  "select count(*) from den_core.task_dependencies",
			TargetCount:  "select count(*) from den_tasks.task_dependencies",
			SourceKeySQL: "select task_id, depends_on from den_core.task_dependencies order by task_id, depends_on",
			TargetKeySQL: "select task_id, depends_on from den_tasks.task_dependencies order by task_id, depends_on",
			Rerun:        recreate,
			SQL: `
insert into den_tasks.task_dependencies(task_id, depends_on)
select task_id, depends_on
from den_core.task_dependencies
order by task_id, depends_on`,
		},
		{
			Domain:       "tasks",
			Source:       "den_core.task_history",
			Target:       "den_tasks.task_history",
			SourceCount:  "select count(*) from den_core.task_history",
			TargetCount:  "select count(*) from den_tasks.task_history",
			SourceKeySQL: "select id from den_core.task_history order by id",
			TargetKeySQL: "select id from den_tasks.task_history order by id",
			Rerun:        recreate,
			SQL: `
insert into den_tasks.task_history(id, task_id, field, old_value, new_value, changed_by, changed_at)
overriding system value
select id, task_id, field, old_value, new_value, changed_by, changed_at::timestamptz
from den_core.task_history
order by id`,
		},
		{
			Domain:       "messages",
			Source:       "den_core.messages",
			Target:       "den_messages.messages",
			SourceCount:  "select count(*) from den_core.messages",
			TargetCount:  "select count(*) from den_messages.messages",
			SourceKeySQL: "select id from den_core.messages order by id",
			TargetKeySQL: "select id from den_messages.messages order by id",
			Rerun:        recreate,
			SQL: `
insert into den_messages.messages(id, project_id, task_id, thread_id, sender, content, intent, metadata, created_at)
overriding system value
select id, project_id, task_id, thread_id, sender, content, intent, metadata, created_at::timestamptz
from den_core.messages
order by id`,
		},
		{
			Domain:       "messages",
			Source:       "den_core.message_reads",
			Target:       "den_messages.message_reads",
			SourceCount:  "select count(*) from den_core.message_reads",
			TargetCount:  "select count(*) from den_messages.message_reads",
			SourceKeySQL: "select message_id, agent from den_core.message_reads order by message_id, agent",
			TargetKeySQL: "select message_id, agent from den_messages.message_reads order by message_id, agent",
			Rerun:        recreate,
			SQL: `
insert into den_messages.message_reads(message_id, agent, read_at)
select message_id, agent, read_at::timestamptz
from den_core.message_reads
order by message_id, agent`,
		},
		{
			Domain:       "documents",
			Source:       "den_core.documents",
			Target:       "den_documents.documents",
			SourceCount:  "select count(*) from den_core.documents",
			TargetCount:  "select count(*) from den_documents.documents",
			SourceKeySQL: "select id from den_core.documents order by id",
			TargetKeySQL: "select id from den_documents.documents order by id",
			Rerun:        recreate,
			SQL: `
insert into den_documents.documents(id, project_id, slug, title, content, doc_type, visibility, tags, summary, created_at, updated_at)
overriding system value
select id, project_id, slug, title, content, doc_type, visibility, tags, summary, created_at::timestamptz, updated_at::timestamptz
from den_core.documents
order by id`,
		},
		{
			Domain:       "documents",
			Source:       "den_core.discussion_threads",
			Target:       "den_documents.discussion_threads",
			SourceCount:  "select count(*) from den_core.discussion_threads",
			TargetCount:  "select count(*) from den_documents.discussion_threads",
			SourceKeySQL: "select id from den_core.discussion_threads order by id",
			TargetKeySQL: "select id from den_documents.discussion_threads order by id",
			Rerun:        recreate,
			SQL: `
insert into den_documents.discussion_threads(id, target_type, target_project_id, target_id, target_slug, target_anchor, thread_key, title, status, created_by, summary, resolution_summary, metadata_json, last_comment_at, created_at, updated_at)
overriding system value
select id, target_type, target_project_id, target_id, target_slug, target_anchor, thread_key, title, status, created_by, summary, resolution_summary, metadata_json, nullif(last_comment_at, '')::timestamptz, created_at::timestamptz, updated_at::timestamptz
from den_core.discussion_threads
order by id`,
		},
		{
			Domain:       "documents",
			Source:       "den_core.discussion_comments",
			Target:       "den_documents.discussion_comments",
			SourceCount:  "select count(*) from den_core.discussion_comments",
			TargetCount:  "select count(*) from den_documents.discussion_comments",
			SourceKeySQL: "select id from den_core.discussion_comments order by id",
			TargetKeySQL: "select id from den_documents.discussion_comments order by id",
			Rerun:        recreate,
			SQL: `
insert into den_documents.discussion_comments(id, thread_id, parent_comment_id, author_identity, body_markdown, comment_kind, status, mentions_json, source_refs_json, metadata_json, created_at, edited_at, updated_at)
overriding system value
select id, thread_id, parent_comment_id, author_identity, body_markdown, comment_kind, status, mentions_json, source_refs_json, metadata_json, created_at::timestamptz, nullif(edited_at, '')::timestamptz, updated_at::timestamptz
from den_core.discussion_comments
order by id`,
		},
		{
			Domain:       "knowledge",
			Source:       "den_core.knowledge_entries",
			Target:       "den_knowledge.knowledge_entries",
			SourceCount:  "select count(*) from den_core.knowledge_entries",
			TargetCount:  "select count(*) from den_knowledge.knowledge_entries",
			SourceKeySQL: "select id from den_core.knowledge_entries order by id",
			TargetKeySQL: "select id from den_knowledge.knowledge_entries order by id",
			Rerun:        recreate,
			SQL: `
insert into den_knowledge.knowledge_entries(id, slug, title, summary, body_markdown, kind, status, curation_state, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, last_reviewed_at, review_due_at, created_by, updated_by, created_at, updated_at)
overriding system value
select id, slug, title, summary, body_markdown, kind, status, curation_state, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, nullif(last_reviewed_at, '')::timestamptz, nullif(review_due_at, '')::timestamptz, created_by, updated_by, created_at::timestamptz, updated_at::timestamptz
from den_core.knowledge_entries
order by id`,
		},
		{
			Domain:       "knowledge",
			Source:       "den_core.knowledge_entry_tags",
			Target:       "den_knowledge.knowledge_entry_tags",
			SourceCount:  "select count(*) from den_core.knowledge_entry_tags",
			TargetCount:  "select count(*) from den_knowledge.knowledge_entry_tags",
			SourceKeySQL: "select entry_id, tag from den_core.knowledge_entry_tags order by entry_id, tag",
			TargetKeySQL: "select entry_id, tag from den_knowledge.knowledge_entry_tags order by entry_id, tag",
			Rerun:        recreate,
			SQL: `
insert into den_knowledge.knowledge_entry_tags(entry_id, tag)
select entry_id, tag
from den_core.knowledge_entry_tags
order by entry_id, tag`,
		},
		{
			Domain:       "knowledge",
			Source:       "den_core.knowledge_entry_revisions",
			Target:       "den_knowledge.knowledge_entry_revisions",
			SourceCount:  "select count(*) from den_core.knowledge_entry_revisions",
			TargetCount:  "select count(*) from den_knowledge.knowledge_entry_revisions",
			SourceKeySQL: "select id from den_core.knowledge_entry_revisions order by id",
			TargetKeySQL: "select id from den_knowledge.knowledge_entry_revisions order by id",
			Rerun:        recreate,
			SQL: `
insert into den_knowledge.knowledge_entry_revisions(id, entry_id, revision_number, title, summary, body_markdown, kind, status, curation_state, tags_json, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, changed_by, change_note, created_at)
overriding system value
select id, entry_id, revision_number, title, summary, body_markdown, kind, status, curation_state, tags_json, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, changed_by, change_note, created_at::timestamptz
from den_core.knowledge_entry_revisions
order by id`,
		},
		{
			Domain:       "knowledge",
			Source:       "den_core.knowledge_entry_links",
			Target:       "den_knowledge.knowledge_entry_links",
			SourceCount:  "select count(*) from den_core.knowledge_entry_links",
			TargetCount:  "select count(*) from den_knowledge.knowledge_entry_links",
			SourceKeySQL: "select id from den_core.knowledge_entry_links order by id",
			TargetKeySQL: "select id from den_knowledge.knowledge_entry_links order by id",
			Rerun:        recreate,
			SQL: `
insert into den_knowledge.knowledge_entry_links(id, from_entry_id, to_entry_slug, link_kind, description, created_at)
overriding system value
select id, from_entry_id, to_entry_slug, link_kind, description, created_at::timestamptz
from den_core.knowledge_entry_links
order by id`,
		},
		{
			Domain:       "review",
			Source:       "den_core.review_rounds",
			Target:       "den_review.review_rounds",
			SourceCount:  "select count(*) from den_core.review_rounds",
			TargetCount:  "select count(*) from den_review.review_rounds",
			SourceKeySQL: "select id from den_core.review_rounds order by id",
			TargetKeySQL: "select id from den_review.review_rounds order by id",
			Rerun:        recreate,
			SQL: `
insert into den_review.review_rounds(id, project_id, task_id, round_number, requested_by, branch, base_branch, base_commit, head_commit, last_reviewed_head_commit, commits_since_last_review, tests_run, notes, preferred_diff_base_ref, preferred_diff_base_commit, preferred_diff_head_ref, preferred_diff_head_commit, alternate_diff_base_ref, alternate_diff_base_commit, alternate_diff_head_ref, alternate_diff_head_commit, delta_base_commit, inherited_commit_count, task_local_commit_count, verdict, verdict_by, verdict_notes, requested_at, verdict_at)
overriding system value
select rr.id, t.project_id, rr.task_id, rr.round_number, rr.requested_by, rr.branch, rr.base_branch, rr.base_commit, rr.head_commit, rr.last_reviewed_head_commit, rr.commits_since_last_review, nullif(rr.tests_run, '')::jsonb, rr.notes, rr.preferred_diff_base_ref, rr.preferred_diff_base_commit, rr.preferred_diff_head_ref, rr.preferred_diff_head_commit, rr.alternate_diff_base_ref, rr.alternate_diff_base_commit, rr.alternate_diff_head_ref, rr.alternate_diff_head_commit, rr.delta_base_commit, rr.inherited_commit_count, rr.task_local_commit_count, rr.verdict, rr.verdict_by, rr.verdict_notes, rr.requested_at::timestamptz, nullif(rr.verdict_at, '')::timestamptz
from den_core.review_rounds rr
join den_core.tasks t on t.id = rr.task_id
order by rr.id`,
		},
		{
			Domain:       "review",
			Source:       "den_core.review_findings",
			Target:       "den_review.review_findings",
			SourceCount:  "select count(*) from den_core.review_findings",
			TargetCount:  "select count(*) from den_review.review_findings",
			SourceKeySQL: "select id from den_core.review_findings order by id",
			TargetKeySQL: "select id from den_review.review_findings order by id",
			Rerun:        recreate,
			SQL: `
insert into den_review.review_findings(id, project_id, finding_key, task_id, review_round_id, finding_number, created_by, category, summary, notes, file_references, test_commands, status, status_updated_by, status_notes, status_updated_at, response_by, response_notes, response_at, follow_up_task_id, created_at, updated_at)
overriding system value
select rf.id, t.project_id, rf.finding_key, rf.task_id, rf.review_round_id, rf.finding_number, rf.created_by, rf.category, rf.summary, rf.notes, rf.file_references, rf.test_commands, rf.status, rf.status_updated_by, rf.status_notes, nullif(rf.status_updated_at, '')::timestamptz, rf.response_by, rf.response_notes, nullif(rf.response_at, '')::timestamptz, rf.follow_up_task_id, rf.created_at::timestamptz, rf.updated_at::timestamptz
from den_core.review_findings rf
join den_core.tasks t on t.id = rf.task_id
order by rf.id`,
		},
	}
}

func skippedSteps() []skippedStep {
	return []skippedStep{
		{
			Domain:    "review",
			Source:    "den_core.review_finding_events",
			Target:    "den_review.review_finding_events",
			Rationale: "source table does not exist in current den_core; target remains empty until successor writes events",
		},
		{
			Domain:    "review",
			Source:    "den_core.review_packets",
			Target:    "den_review.review_packets",
			Rationale: "source table does not exist in current den_core; packet history remains in den_core.messages metadata for this import",
		},
	}
}

func truncateTargetsSQL() string {
	return `
truncate
    den_review.review_packets,
    den_review.review_finding_events,
    den_review.review_findings,
    den_review.review_rounds,
    den_knowledge.knowledge_entry_links,
    den_knowledge.knowledge_entry_revisions,
    den_knowledge.knowledge_entry_tags,
    den_knowledge.knowledge_entries,
    den_documents.discussion_comments,
    den_documents.discussion_threads,
    den_documents.documents,
    den_messages.message_reads,
    den_messages.messages,
    den_tasks.task_dependencies,
    den_tasks.task_history,
    den_tasks.tasks,
    den_projects.projects
restart identity cascade`
}

func resetSequencesSQL() string {
	return `
select setval(pg_get_serial_sequence('den_tasks.tasks', 'id'), coalesce((select max(id) from den_tasks.tasks), 1), (select max(id) is not null from den_tasks.tasks));
select setval(pg_get_serial_sequence('den_tasks.task_history', 'id'), coalesce((select max(id) from den_tasks.task_history), 1), (select max(id) is not null from den_tasks.task_history));
select setval(pg_get_serial_sequence('den_messages.messages', 'id'), coalesce((select max(id) from den_messages.messages), 1), (select max(id) is not null from den_messages.messages));
select setval(pg_get_serial_sequence('den_documents.documents', 'id'), coalesce((select max(id) from den_documents.documents), 1), (select max(id) is not null from den_documents.documents));
select setval(pg_get_serial_sequence('den_documents.discussion_threads', 'id'), coalesce((select max(id) from den_documents.discussion_threads), 1), (select max(id) is not null from den_documents.discussion_threads));
select setval(pg_get_serial_sequence('den_documents.discussion_comments', 'id'), coalesce((select max(id) from den_documents.discussion_comments), 1), (select max(id) is not null from den_documents.discussion_comments));
select setval(pg_get_serial_sequence('den_knowledge.knowledge_entries', 'id'), coalesce((select max(id) from den_knowledge.knowledge_entries), 1), (select max(id) is not null from den_knowledge.knowledge_entries));
select setval(pg_get_serial_sequence('den_knowledge.knowledge_entry_revisions', 'id'), coalesce((select max(id) from den_knowledge.knowledge_entry_revisions), 1), (select max(id) is not null from den_knowledge.knowledge_entry_revisions));
select setval(pg_get_serial_sequence('den_knowledge.knowledge_entry_links', 'id'), coalesce((select max(id) from den_knowledge.knowledge_entry_links), 1), (select max(id) is not null from den_knowledge.knowledge_entry_links));
select setval(pg_get_serial_sequence('den_review.review_rounds', 'id'), coalesce((select max(id) from den_review.review_rounds), 1), (select max(id) is not null from den_review.review_rounds));
select setval(pg_get_serial_sequence('den_review.review_findings', 'id'), coalesce((select max(id) from den_review.review_findings), 1), (select max(id) is not null from den_review.review_findings));
select setval(pg_get_serial_sequence('den_review.review_finding_events', 'id'), coalesce((select max(id) from den_review.review_finding_events), 1), (select max(id) is not null from den_review.review_finding_events));
select setval(pg_get_serial_sequence('den_review.review_packets', 'id'), coalesce((select max(id) from den_review.review_packets), 1), (select max(id) is not null from den_review.review_packets));`
}
