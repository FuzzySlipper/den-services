package ops

import (
	"context"
	"fmt"
)

// LedgerCheck is what a read of a whole ledger found: SQLite's own integrity
// verdict and how many records each table holds.
type LedgerCheck struct {
	Integrity     string
	Hosts         int
	Jobs          int
	Runs          int
	Verifications int
}

// ExportTo writes a consistent copy of the ledger to path, which must not
// exist yet. VACUUM INTO reads one snapshot, so concurrent reports are safe.
func (s *Store) ExportTo(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "vacuum into ?", path); err != nil {
		return fmt.Errorf("vacuum into %s: %w", path, err)
	}
	return nil
}

// CheckLedger runs integrity_check and counts every table.
func (s *Store) CheckLedger(ctx context.Context) (LedgerCheck, error) {
	var check LedgerCheck
	if err := s.db.QueryRowContext(ctx, "pragma integrity_check").Scan(&check.Integrity); err != nil {
		return check, fmt.Errorf("integrity_check: %w", err)
	}
	for table, count := range map[string]*int{
		"hosts": &check.Hosts, "jobs": &check.Jobs, "runs": &check.Runs, "verifications": &check.Verifications,
	} {
		if err := s.db.QueryRowContext(ctx, "select count(*) from "+table).Scan(count); err != nil {
			return check, fmt.Errorf("counting %s: %w", table, err)
		}
	}
	return check, nil
}
