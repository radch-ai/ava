package mcpservers

// Security decisions this package makes are recorded through the shared
// audit shape: a destination/plaintext approval, a revocation.
// The records hold identifiers and static codes only.

import (
	"context"
	"errors"

	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// auditDecision writes one point-in-time record.
func auditDecision(ctx context.Context, queries *db.Queries, decision tooladmission.Decision) error {
	return tooladmission.NewAuditor(queries).Record(ctx, decision)
}

// auditApproval records a destination/plaintext approval or its removal.
func auditApproval(ctx context.Context, queries *db.Queries, serverID, event, outcome, reason string) error {
	return auditDecision(ctx, queries, tooladmission.Decision{
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
	rows, err := queries.ListMcpServerTools(ctx, serverID)
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
		if err := auditApproval(ctx, queries, serverID, "approval_change", "approve",
			"destination_approved"); err != nil {
			return err
		}
	}
	if allowPlain {
		if err := auditApproval(ctx, queries, serverID, "plaintext_exception", "approve",
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
		if err := auditApproval(ctx, queries, serverID, "approval_change", "revoke",
			"destination_not_activated"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if allowPlain {
		if err := auditApproval(ctx, queries, serverID, "plaintext_exception", "revoke",
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
		if err := auditApproval(ctx, queries, serverID, "approval_change", "revoke",
			"destination_revoked"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if plaintext {
		if err := auditApproval(ctx, queries, serverID, "plaintext_exception", "revoke",
			"plaintext_revoked"); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
