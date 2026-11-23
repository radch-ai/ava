// Shared grant/revoke toggle mechanics: the invariant every
// approval-shaped switch follows, in one place.
package api

import (
	"context"
	"database/sql"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// ApplyToggle writes one approval-shaped toggle. err is a hard failure
// (the switch did not move, or — on activation — its record did not
// commit); auditErr is set only when the restriction landed and its
// record failed, so the caller answers success with the degraded trail
// joined in.
//
// enabled=true is an activation: the write and its approval-change
// record commit together, so privilege is never live unrecorded.
// enabled=false is a restriction: the write commits on its own, cancel
// runs (admitted work is revoked) before the record is attempted.
//
// write receives the queries the arm uses (the transaction's on the
// activation arm). A target row that vanished under a concurrent delete
// is a harmless no-op: the caller's own pre-read already 404s.
func ApplyToggle(ctx context.Context, sqlDB *sql.DB, queries *db.Queries, enabled bool,
	d tooladmission.Decision, write func(runner *db.Queries) error, cancel func()) (auditErr, err error) {
	if !enabled {
		if err := write(queries); err != nil {
			return nil, err
		}
		if cancel != nil {
			cancel()
		}
		if auditErr := tooladmission.RecordDecision(ctx, queries, d); auditErr != nil {
			return auditErr, nil
		}
		return nil, nil
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txq := db.New(tx)
	if err := write(txq); err != nil {
		return nil, err
	}
	if err := tooladmission.RecordDecision(ctx, txq, d); err != nil {
		return nil, err // the deferred rollback discards the tx
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nil, nil
}
