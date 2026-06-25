# den-core Phase 0A Import/Parity Dry Run

Task: den-core #3319.

This command proves that a copied den-core SQLite database can populate the temporary Postgres `den_core` schema and pass basic parity checks. It is an offline proofing tool, not a live cutover tool.

## Safety Rules

- Use a SQLite backup or copy. Do not point the tool at a live writable SQLite file.
- Use a staging or disposable Postgres target unless the operator explicitly intends to reset `den_core`.
- The command opens SQLite read-only and never writes to it.
- The command only truncates Postgres `den_core` tables when `--reset-target` is passed.
- Do not run this during a live cutover window without the separate Phase 0D runbook and explicit approval.

## Example

```bash
cd /home/dev/den-services/migration

go run ./cmd/den-core-import-parity \
  --source-sqlite /path/to/den-core-backup.db \
  --postgres-url "$DEN_CORE_MIGRATION_DATABASE_URL" \
  --apply-migrations \
  --reset-target
```

`--apply-migrations` applies the embedded `den_core` migrations only. It does not run the full den-services migration set.

The report prints table counts, imported rows, ID ranges, checksum status, JSON/timestamp parse failures, FK anomalies, and known caveats. It exits non-zero on unexplained schema or parity mismatches.

## Known Caveat

Document and knowledge search parity is placeholder-only in Phase 0A. SQLite FTS5 virtual tables are intentionally excluded from import; Postgres `tsvector` columns exist as the draft shape, and task #3324 owns final search/ranking/snippet parity.
