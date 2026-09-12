package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	corepolicy "fluxen/pkg/policy"
	"fluxen/pkg/types"
)

// ErrVersionConflict is returned by Store.Save when the caller's expected
// current version no longer matches what's persisted — another apply/save
// landed first. The caller should re-read and retry rather than silently
// overwrite that change.
var ErrVersionConflict = errors.New("policy: version conflict, re-read and retry")

// Record is one application's current policy — an application with no
// row yet behaves exactly like an empty, all-disabled document (Part L
// Phase 5: "an application with no policy document behaves exactly like
// Phase 1's pipeline").
type Record struct {
	AppID     types.AppID
	Version   int
	Document  corepolicy.PolicyDocument
	UpdatedAt time.Time
	UpdatedBy *types.UserID
}

// HistoryEntry is one row of policy_history — every mutation, whatever
// its source, append-only.
type HistoryEntry struct {
	ID            string
	AppID         types.AppID
	Version       int
	Document      corepolicy.PolicyDocument
	Diff          json.RawMessage
	ChangeSource  string
	OpportunityID *string
	SimulationID  *string
	Note          *string
	ChangedBy     *types.UserID
	ChangedAt     time.Time
}

// Store is the Postgres-backed read/write path for policies and their
// history.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get returns an application's current policy, or the zero-version empty
// document if it has never had one written.
func (s *Store) Get(ctx context.Context, appID types.AppID) (Record, error) {
	var rec Record
	var doc []byte
	err := s.pool.QueryRow(ctx, `
		SELECT app_id, version, document, updated_at, updated_by
		FROM policies WHERE app_id = $1
	`, appID).Scan(&rec.AppID, &rec.Version, &doc, &rec.UpdatedAt, &rec.UpdatedBy)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Record{AppID: appID, Version: 0, Document: corepolicy.PolicyDocument{}}, nil
		}
		return Record{}, fmt.Errorf("policy: failed to get policy: %w", err)
	}
	if err := json.Unmarshal(doc, &rec.Document); err != nil {
		return Record{}, fmt.Errorf("policy: failed to unmarshal stored document: %w", err)
	}
	return rec, nil
}

// Save writes a new version of an application's policy and appends the
// corresponding history row, in one transaction. expectedVersion must
// equal the version Save's caller last read (0 if the app has never had
// a policy) — a mismatch means someone else's write landed first and
// this one is rejected with ErrVersionConflict rather than silently
// clobbering it.
func (s *Store) Save(ctx context.Context, appID types.AppID, expectedVersion int, doc corepolicy.PolicyDocument, entry HistoryEntry) (Record, error) {
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to marshal document: %w", err)
	}
	if entry.Diff == nil {
		entry.Diff = json.RawMessage(`{}`)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if Commit already succeeded

	newVersion := expectedVersion + 1
	var rec Record
	var storedDoc []byte
	if expectedVersion == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO policies (app_id, version, document, updated_by)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (app_id) DO NOTHING
			RETURNING app_id, version, document, updated_at, updated_by
		`, appID, newVersion, docJSON, entry.ChangedBy).Scan(&rec.AppID, &rec.Version, &storedDoc, &rec.UpdatedAt, &rec.UpdatedBy)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE policies SET version = $2, document = $3, updated_at = now(), updated_by = $4
			WHERE app_id = $1 AND version = $5
			RETURNING app_id, version, document, updated_at, updated_by
		`, appID, newVersion, docJSON, entry.ChangedBy, expectedVersion).Scan(&rec.AppID, &rec.Version, &storedDoc, &rec.UpdatedAt, &rec.UpdatedBy)
	}
	if err == pgx.ErrNoRows {
		return Record{}, ErrVersionConflict
	}
	if err != nil {
		return Record{}, fmt.Errorf("policy: failed to save policy: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO policy_history (app_id, version, document, diff, change_source, opportunity_id, simulation_id, note, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, appID, newVersion, docJSON, entry.Diff, entry.ChangeSource, entry.OpportunityID, entry.SimulationID, entry.Note, entry.ChangedBy); err != nil {
		return Record{}, fmt.Errorf("policy: failed to write policy history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("policy: failed to commit policy save: %w", err)
	}

	if err := json.Unmarshal(storedDoc, &rec.Document); err != nil {
		return Record{}, fmt.Errorf("policy: failed to unmarshal saved document: %w", err)
	}
	return rec, nil
}

// History returns an application's policy_history, newest version first.
func (s *Store) History(ctx context.Context, appID types.AppID) ([]HistoryEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, app_id, version, document, diff, change_source, opportunity_id, simulation_id, note, changed_by, changed_at
		FROM policy_history
		WHERE app_id = $1
		ORDER BY version DESC
	`, appID)
	if err != nil {
		return nil, fmt.Errorf("policy: failed to load policy history: %w", err)
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var doc []byte
		if err := rows.Scan(&e.ID, &e.AppID, &e.Version, &doc, &e.Diff, &e.ChangeSource, &e.OpportunityID, &e.SimulationID, &e.Note, &e.ChangedBy, &e.ChangedAt); err != nil {
			return nil, fmt.Errorf("policy: failed to scan policy history row: %w", err)
		}
		if err := json.Unmarshal(doc, &e.Document); err != nil {
			return nil, fmt.Errorf("policy: failed to unmarshal history document: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy: failed to load policy history: %w", err)
	}
	return out, nil
}

// AtVersion returns the document stored in history for a specific
// version — used by Revert to restore a prior document.
func (s *Store) AtVersion(ctx context.Context, appID types.AppID, version int) (corepolicy.PolicyDocument, error) {
	var doc []byte
	err := s.pool.QueryRow(ctx, `
		SELECT document FROM policy_history WHERE app_id = $1 AND version = $2
	`, appID, version).Scan(&doc)
	if err != nil {
		if err == pgx.ErrNoRows {
			return corepolicy.PolicyDocument{}, fmt.Errorf("policy: no history entry at version %d", version)
		}
		return corepolicy.PolicyDocument{}, fmt.Errorf("policy: failed to load historical document: %w", err)
	}
	var out corepolicy.PolicyDocument
	if err := json.Unmarshal(doc, &out); err != nil {
		return corepolicy.PolicyDocument{}, fmt.Errorf("policy: failed to unmarshal historical document: %w", err)
	}
	return out, nil
}

// ChangedSince reports whether an application's policy has changed
// (a new version landed) after atVersion and before the given instant —
// Phase 6's confound detection (Part G.5: "a second policy change landed
// inside the measurement window" makes a verdict inconclusive, since
// there's no way to attribute an observed cost change to one control
// change over the other).
func (s *Store) ChangedSince(ctx context.Context, appID types.AppID, atVersion int, before time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM policy_history
			WHERE app_id = $1 AND version > $2 AND changed_at < $3
		)
	`, appID, atVersion, before).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("policy: failed to check for a confounding policy change: %w", err)
	}
	return exists, nil
}
