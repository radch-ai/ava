package mcpservers

// Security decisions this package makes are recorded through the shared
// audit shape: a destination/plaintext approval, a revocation.
// The records hold identifiers and static codes only.

import (
	"context"
	"database/sql"
	"errors"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// auditApproval records one approval-shaped decision: the global/server
// switch, a destination approval or its removal.
func auditApproval(ctx context.Context, queries *db.Queries, serverID, outcome, reason string) error {
	return auditDecision(ctx, queries, tooladmission.EventApprovalChange, serverID, outcome, reason)
}

// auditPlaintext records the plaintext-credential exception's own trail.
func auditPlaintext(ctx context.Context, queries *db.Queries, serverID, outcome, reason string) error {
	return auditDecision(ctx, queries, tooladmission.EventPlaintext, serverID, outcome, reason)
}

// auditDecision writes one point-in-time record.
func auditDecision(ctx context.Context, queries *db.Queries, event, serverID, outcome, reason string) error {
	return tooladmission.RecordDecision(ctx, queries, tooladmission.Decision{
		Event:    event,
		ServerID: serverID,
		Outcome:  outcome,
		Reason:   reason,
	})
}

// cancelRemovedTools cancels the in-flight calls of the tools this
// publish deleted: a tool the server no longer reports must not keep
// running. Tools that survived the publish are untouched — a refreshed
// definition does not interrupt a healthy call.
func cancelRemovedTools(canceler api.Canceler, serverID string, before, kept map[string]struct{}) {
	if canceler == nil {
		return
	}
	for id := range before {
		if _, survives := kept[id]; !survives {
			canceler.CancelToolCalls(serverID, id)
		}
	}
}

// toolIDs snapshots one server's catalog ids before a publish, so a
// removal can be detected afterwards.
func toolIDs(ctx context.Context, queries *db.Queries, serverID string) map[string]struct{} {
	rows, err := queries.ListMcpServerTools(ctx, sql.NullString{String: serverID, Valid: true})
	if err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		out[row.ID] = struct{}{}
	}
	return out
}

// keptFromListing names the catalog ids a listing retains.
func keptFromListing(serverID string, listed []mcp.ListedTool) map[string]struct{} {
	kept := make(map[string]struct{}, len(listed))
	for _, tool := range listed {
		kept[mcp.CatalogID(serverID, tool.Remote)] = struct{}{}
	}
	return kept
}

// approveAuthority writes the approval records a new destination/plaintext
// authority requires, before that authority is exercised. The first
// failure stops the caller: unauditable authority must not activate.
func approveAuthority(ctx context.Context, queries *db.Queries, serverID string, allowPrivate, allowPlain bool) error {
	if allowPrivate {
		if err := auditApproval(ctx, queries, serverID, "approve",
			"destination_approved"); err != nil {
			return err
		}
	}
	if allowPlain {
		if err := auditPlaintext(ctx, queries, serverID, "approve",
			"plaintext_approved"); err != nil {
			return err
		}
	}
	return nil
}

// withdrawAuthority records that approvals written by approveAuthority
// never activated — verification or a write failed, so no privilege
// exists. Log-only: the caller is already answering an error.
func withdrawAuthority(ctx context.Context, queries *db.Queries, serverID string, allowPrivate, allowPlain bool) error {
	var errs error
	if allowPrivate {
		if err := auditApproval(ctx, queries, serverID, "revoke",
			"destination_not_activated"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if allowPlain {
		if err := auditPlaintext(ctx, queries, serverID, "revoke",
			"plaintext_not_activated"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// revokeAuthority records the removal of a destination/plaintext authority
// that was active: the tightening already took effect, so a failure here
// only degrades the trail.
func revokeAuthority(ctx context.Context, queries *db.Queries, serverID string, destination, plaintext bool) error {
	var errs error
	if destination {
		if err := auditApproval(ctx, queries, serverID, "revoke",
			"destination_revoked"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if plaintext {
		if err := auditPlaintext(ctx, queries, serverID, "revoke",
			"plaintext_revoked"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
