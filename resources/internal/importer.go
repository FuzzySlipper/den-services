package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// ImportSummary counts what ImportBoard copied and what it left alone.
type ImportSummary struct {
	Hosts            int `json:"hosts"`
	Resources        int `json:"resources"`
	ResourcesSkipped int `json:"resources_skipped"`
	Work             int `json:"work"`
	WorkSkipped      int `json:"work_skipped"`
}

// legacyResource and legacyWork are the JSON rows of the asset-pipeline
// Resource Board (tools/resource-board/store.py); times are Unix seconds.
type legacyResource struct {
	ID                 string   `json:"id"`
	Host               string   `json:"host"`
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	Path               string   `json:"path"`
	ProbePath          string   `json:"probe_path"`
	RuntimePath        string   `json:"runtime_path"`
	Access             string   `json:"access"`
	Sharing            string   `json:"sharing"`
	Lifecycle          string   `json:"lifecycle"`
	Notes              string   `json:"notes"`
	Guide              string   `json:"guide"`
	Match              string   `json:"match"`
	UpdatedAt          *float64 `json:"updated_at"`
	LastUsedAt         *float64 `json:"last_used_at"`
	LastUsedBy         string   `json:"last_used_by"`
	ObservedActivityAt *float64 `json:"observed_activity_at"`
	ConfirmedAt        *float64 `json:"confirmed_at"`
	ConfirmedBy        string   `json:"confirmed_by"`
	PathPresent        *bool    `json:"path_present"`
	PathCheckedAt      *float64 `json:"path_checked_at"`
}

type legacyWork struct {
	ID         string   `json:"id"`
	Host       string   `json:"host"`
	Source     string   `json:"source"`
	ExternalID *string  `json:"external_id"`
	Title      string   `json:"title"`
	Owner      string   `json:"owner"`
	Project    string   `json:"project"`
	Resources  []string `json:"resources"`
	State      string   `json:"state"`
	Note       string   `json:"note"`
	URL        string   `json:"url"`
	TTL        float64  `json:"ttl"`
	CreatedAt  float64  `json:"created_at"`
	StartedAt  *float64 `json:"started_at"`
	UpdatedAt  float64  `json:"updated_at"`
	ExpiresAt  float64  `json:"expires_at"`
	EndedAt    *float64 `json:"ended_at"`
}

// ImportBoard copies the old board's resources and work ledger. Rows that
// already exist here are left alone, so it is safe to run again. Hosts named
// by the old rows are registered with their ID as the name until the
// collector registers them properly.
func ImportBoard(ctx context.Context, store *Store, legacyPath string, now time.Time) (ImportSummary, error) {
	legacy, err := sql.Open("sqlite", "file:"+legacyPath+"?mode=ro")
	if err != nil {
		return ImportSummary{}, fmt.Errorf("opening %s: %w", legacyPath, err)
	}
	defer legacy.Close()
	var resources []legacyResource
	if err := readLegacy(ctx, legacy, "resources", &resources); err != nil {
		return ImportSummary{}, err
	}
	var work []legacyWork
	if err := readLegacy(ctx, legacy, "work", &work); err != nil {
		return ImportSummary{}, err
	}
	var summary ImportSummary
	err = store.InTx(ctx, func(tx *Tx) error {
		ensureHost := func(id string) error {
			if host, err := tx.Host(id); err != nil || host != nil {
				return err
			}
			summary.Hosts++
			return tx.PutHost(&Host{ID: id, Name: id, Position: 100, CreatedAt: now, UpdatedAt: now})
		}
		for _, row := range resources {
			if existing, err := tx.Resource(row.ID); err != nil {
				return err
			} else if existing != nil {
				summary.ResourcesSkipped++
				continue
			}
			if err := ensureHost(row.Host); err != nil {
				return err
			}
			lifecycle := Lifecycle(row.Lifecycle)
			if !lifecycle.IsValid() {
				lifecycle = LifecycleUnknown
			}
			updated := now
			if row.UpdatedAt != nil {
				updated = unixTime(*row.UpdatedAt)
			}
			if err := tx.PutResource(&Resource{
				ID: row.ID, Host: row.Host, Name: row.Name, Kind: firstNonEmpty(row.Kind, defaultResourceKind),
				Path: row.Path, ProbePath: row.ProbePath, RuntimePath: row.RuntimePath, Access: row.Access,
				Sharing: row.Sharing, Lifecycle: lifecycle, Notes: row.Notes, Guide: row.Guide, Match: row.Match,
				UpdatedAt: updated, LastUsedAt: optionalUnix(row.LastUsedAt), LastUsedBy: row.LastUsedBy,
				ObservedActivityAt: optionalUnix(row.ObservedActivityAt), ConfirmedAt: optionalUnix(row.ConfirmedAt),
				ConfirmedBy: row.ConfirmedBy, PathPresent: row.PathPresent, PathCheckedAt: optionalUnix(row.PathCheckedAt),
			}); err != nil {
				return err
			}
			summary.Resources++
		}
		for _, row := range work {
			if existing, err := tx.Work(row.ID); err != nil {
				return err
			} else if existing != nil {
				summary.WorkSkipped++
				continue
			}
			if row.ExternalID != nil {
				if existing, err := tx.WorkByExternalID(row.Source, row.Host, *row.ExternalID); err != nil {
					return err
				} else if existing != nil {
					summary.WorkSkipped++
					continue
				}
			}
			if err := ensureHost(row.Host); err != nil {
				return err
			}
			state := WorkState(row.State)
			if !state.IsValid() {
				state = WorkFailed
			}
			if err := tx.PutWork(&Work{
				ID: row.ID, Host: row.Host, Source: firstNonEmpty(row.Source, "check-in"), ExternalID: row.ExternalID,
				Title: row.Title, Owner: row.Owner, Project: row.Project, Resources: nonNil(row.Resources),
				State: state, Note: row.Note, URL: row.URL, TTL: time.Duration(row.TTL * float64(time.Second)),
				CreatedAt: unixTime(row.CreatedAt), StartedAt: optionalUnix(row.StartedAt),
				UpdatedAt: unixTime(row.UpdatedAt), ExpiresAt: unixTime(row.ExpiresAt), EndedAt: optionalUnix(row.EndedAt),
			}); err != nil {
				return err
			}
			summary.Work++
		}
		return nil
	})
	return summary, err
}

func readLegacy[T any](ctx context.Context, db *sql.DB, table string, target *[]T) error {
	rows, err := db.QueryContext(ctx, "select data from "+table) //nolint:gosec // table is one of two constants
	if err != nil {
		return fmt.Errorf("reading legacy %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return fmt.Errorf("scanning legacy %s: %w", table, err)
		}
		var row T
		if err := json.Unmarshal([]byte(data), &row); err != nil {
			return fmt.Errorf("decoding legacy %s row: %w", table, err)
		}
		*target = append(*target, row)
	}
	return rows.Err()
}

func unixTime(seconds float64) time.Time {
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*1e9)).UTC()
}

func optionalUnix(seconds *float64) *time.Time {
	if seconds == nil {
		return nil
	}
	at := unixTime(*seconds)
	return &at
}
