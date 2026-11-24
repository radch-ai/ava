// PATCH /api/mcp/servers/{id} - edit a server with absent-keeps
// semantics: a present headers map replaces the stored one
// wholesale, an absent field keeps it. Connection-affecting edits are
// re-verified before anything is written; a rename touches neither the
// vault nor the live connection. A URL change invalidates the destination
// and plaintext approvals unless the same patch re-approves them.
package mcpservers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/names"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Update applies one patch. Renames without connection changes skip the
// re-verify, the vault, and discovery; anything that changes what a dial
// looks like proves the new config connects before a single write
// happens.
func Update(throttle *api.Throttle, manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store, canceler api.Canceler, logger zerolog.Logger) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.UpdateMcpServerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")

		row, ok, fault := readServer(r.Context(), queries, w, id)
		if !ok {
			return fault
		}
		if !throttle.Allow("mcp:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Editing too fast — try again in a moment")
		}

		next, problem := applyPatch(row, req)
		if problem != "" {
			return api.WriteError(w, http.StatusBadRequest, problem)
		}
		transportChanged := next.transport != row.Transport

		// A stdio process edit needs the acknowledgment: the fingerprint
		// covers command and args, so either changing re-prompts. An
		// unchanged process keeps its stored acknowledgment.
		ackHash := ""
		ackChanged := false
		if next.transport == mcp.TransportStdio {
			ackHash = processAck(next.command, next.args)
			ackChanged = !row.ProcessAck.Valid || row.ProcessAck.String != ackHash
			if ackChanged && !req.AcknowledgeProcessExecution {
				return api.WriteError(w, http.StatusBadRequest, ackRequired)
			}
		} else if req.AcknowledgeProcessExecution {
			return api.WriteError(w, http.StatusBadRequest, ackNotApplicable)
		}

		// A URL move invalidates the approvals bound to the old endpoint:
		// the new endpoint's approvals are new authority even when the flag
		// values themselves are unchanged.
		urlMoved := !sameNullString(next.url, row.Url)
		privateAdded := next.allowPrivateNetwork && (row.AllowPrivateNetwork == 0 || urlMoved)
		plainAdded := next.allowPlaintext && (row.AllowPlaintext == 0 || urlMoved)
		privateRemoved := row.AllowPrivateNetwork != 0 && !next.allowPrivateNetwork
		plainRemoved := row.AllowPlaintext != 0 && !next.allowPlaintext
		authorityAdded := privateAdded || plainAdded
		// The connection shape excludes the two policy flags: a flag
		// removal moves them, and must still persist without a dial.
		shapeChanged := urlMoved || transportChanged || !sameNullInt64(next.timeout, row.TimeoutMs)
		disabling := req.Enabled != nil && !*req.Enabled
		enabling := req.Enabled != nil && *req.Enabled
		// A process edit is a connection edit even when the target is
		// unreachable, so it never rides the no-dial withdrawal path.
		processTouched := req.Command != nil || req.Args != nil || req.InheritEnv != nil
		// Narrowing authority — or switching the server off — needs no
		// successful connection check: an unreachable or newly forbidden
		// endpoint must not be able to block its own restriction. Any
		// connection-shape change (a URL move, a timeout change, a
		// transport switch) still verifies: a disable must not smuggle an
		// unverified endpoint, and a patch that also re-enables takes the
		// connection path, so the reconnect is never skipped.
		withdrawalOnly := !authorityAdded && req.Headers == nil && req.Env == nil &&
			!processTouched && !transportChanged && !enabling && !shapeChanged &&
			(disabling || privateRemoved || plainRemoved)
		connectionEdit := (next.connectionChanged || req.Headers != nil || req.Env != nil) && !withdrawalOnly

		var secrets, storedSecrets mcpserver.Secrets
		secretsReplaced := false
		if connectionEdit {
			var err error
			// One unseal path: mcpserver.Config reads the stored blob
			// with the same missing-means-empty rule.
			storedCfg, err := mcpserver.Config(r.Context(), row, vaultStore)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("unseal secrets for %q: %w", id, err)
			}
			secrets = mcpserver.Secrets{Headers: storedCfg.Headers, Env: storedCfg.Env}
			// The stored values ride along in case the row write below
			// fails after a successful re-seal and must be restored.
			storedSecrets = secrets
			if req.Headers != nil {
				secrets.Headers = req.Headers
				secretsReplaced = true
			}
			if req.Env != nil {
				secrets.Env = req.Env
				secretsReplaced = true
			}
			if transportChanged {
				// The blob belongs to one transport: the old one's map
				// must not linger as a credential the new config
				// rejects.
				if next.transport == mcp.TransportStdio {
					secrets.Headers = nil
				} else {
					secrets.Env = nil
				}
				secretsReplaced = true
			}
		}

		// The key-name columns follow the effective secrets; a rename
		// passes the stored names through untouched.
		headerKeys, envKeys := row.HeaderKeys, row.EnvKeys
		if connectionEdit {
			headerKeys = mcpserver.HeaderKeys(secrets.Headers)
			envKeys = mcpserver.EnvKeys(secrets.Env)
		}
		// The process column follows the effective transport: a changed
		// process stores the acknowledged fingerprint, an http row none.
		processAckCol := sql.NullString{}
		switch {
		case next.transport != mcp.TransportStdio:
		case ackChanged:
			processAckCol = sql.NullString{String: ackHash, Valid: true}
		default:
			processAckCol = row.ProcessAck
		}
		// A pure switch patch writes no config: enabled lives on its own
		// query (mcp_servers.sql), and the row is otherwise untouched.
		// Replaced secret maps do write: their key-name columns change.
		configTouched := secretsReplaced || next.connectionChanged || req.Name != nil ||
			req.URL != nil || req.TimeoutMs != nil || req.ClearTimeoutMs ||
			req.AllowPrivateNetwork != nil || req.AllowPlaintext != nil ||
			req.Transport != nil || req.Command != nil || req.Args != nil ||
			req.InheritEnv != nil

		// writeRow seals the replaced secrets and persists the row. The
		// row write is the commit point; a failure restores the blob it
		// replaced (the single-entry rule) and reports both.
		writeRow := func() error {
			if secretsReplaced {
				if err := sealSecrets(r.Context(), vaultStore, id, secrets); err != nil {
					return fmt.Errorf("seal secrets for %q: %w", id, err)
				}
			}
			if !configTouched {
				return nil
			}
			if err := queries.UpdateMcpServer(r.Context(), db.UpdateMcpServerParams{
				Name:                next.name,
				Transport:           next.transport,
				Command:             storedCommand(next.transport, next.command),
				Args:                mcpserver.EncodeStrings(next.args),
				EnvKeys:             envKeys,
				InheritEnv:          boolInt(next.inheritEnv),
				ProcessAck:          processAckCol,
				Url:                 next.url,
				HeaderKeys:          headerKeys,
				TimeoutMs:           next.timeout,
				AllowPrivateNetwork: boolInt(next.allowPrivateNetwork),
				AllowPlaintext:      boolInt(next.allowPlaintext),
				ID:                  id,
			}); err != nil {
				var rollback error
				if secretsReplaced {
					if rbErr := sealSecrets(r.Context(), vaultStore, id, storedSecrets); rbErr != nil {
						rollback = fmt.Errorf("restore sealed secrets for %q: %w", id, rbErr)
					}
				}
				return errors.Join(fmt.Errorf("update mcp server %q: %w", id, err), rollback)
			}
			return nil
		}

		// toggle applies the global switch through the shared grant/revoke
		// mechanics: enabling commits the switch and its record together,
		// disabling commits the switch, the client teardown and the
		// revocation first, and answers a failed record as auditErr — the
		// switch already moved, so that failure never denies the save.
		toggle := func() (auditErr, err error) {
			if req.Enabled == nil {
				return nil, nil
			}
			enabled := int64(0)
			outcome, reason := "revoke", "server_disabled"
			if *req.Enabled {
				enabled, outcome, reason = 1, "approve", "server_enabled"
			}
			return api.ApplyToggle(r.Context(), sqlDB, queries, *req.Enabled,
				tooladmission.Decision{
					Event:    tooladmission.EventApprovalChange,
					ServerID: id,
					Outcome:  outcome,
					Reason:   reason,
				},
				func(runner *db.Queries) error {
					_, err := runner.ToggleMcpServerEnabled(r.Context(), db.ToggleMcpServerEnabledParams{
						Enabled: enabled, ID: id,
					})
					return err
				},
				// Disabling revokes: the live client goes and admitted
				// calls are cancelled.
				func() {
					manager.Close(id)
					if canceler != nil {
						canceler.CancelServerTools(id)
					}
				})
		}

		// respond renders the saved row with the listing the pass knows.
		respond := func(status int, listed []mcp.ListedTool, skipped []mcp.Skipped) error {
			return respondServer(r.Context(), w, status, queries, id, listed, skipped)
		}

		if withdrawalOnly {
			// The tightening persists with no dial at all, and the live
			// client is closed so the next enable reconnects under the
			// new policy: a switch toggle's teardown rides ApplyToggle's
			// cancel callback, a pure flag removal closes it here. The
			// restriction takes effect even when its audit record fails —
			// the failure degrades the trail, surfaced through the joined
			// error, never the save.
			before := toolIDs(r.Context(), queries, id)
			if err := writeRow(); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			if req.Enabled == nil {
				// A pure flag withdrawal carries no switch toggle.
				manager.Close(id)
				if canceler != nil {
					canceler.CancelServerTools(id)
				}
			}
			toggleAudit, err := toggle()
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			listed, err := catalogListing(r.Context(), queries, id)
			if err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
			cancelRemovedTools(canceler, id, before, keptFromListing(id, listed))
			return errors.Join(
				respond(http.StatusOK, listed, nil),
				revokeAuthority(r.Context(), queries, id, privateRemoved, plainRemoved),
				toggleAudit)
		}

		// Newly granted authority is recorded before the credential or
		// process it implies is exercised — verification included.
		if err := approveAuthority(r.Context(), queries, id, privateAdded, plainAdded); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "audit unavailable")
			return fmt.Errorf("audit approvals for %q: %w", id, err)
		}
		ackRecorded := false
		if ackChanged {
			if err := auditApproval(r.Context(), queries, id, "approve", "process_acknowledged"); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "audit unavailable")
				return errors.Join(
					fmt.Errorf("audit process acknowledgment for %q: %w", id, err),
					withdrawAuthority(r.Context(), queries, id, privateAdded, plainAdded))
			}
			ackRecorded = true
		}
		// Nothing of this patch installs on a rejection: authority that
		// never activated is withdrawn with an explicit record.
		withdraw := func() error {
			var errs error
			if ackRecorded {
				errs = errors.Join(errs, auditApproval(r.Context(), queries, id,
					"revoke", "process_ack_not_activated"))
			}
			if authorityAdded {
				errs = errors.Join(errs, withdrawAuthority(r.Context(), queries, id, privateAdded, plainAdded))
			}
			return errs
		}

		// The candidate config dials with the effective secrets; the
		// verified client is installed after the catalog write so publication
		// publishes the verified listing instead of listing twice.
		var (
			verified  *mcp.Client
			verifyCfg mcp.ServerConfig
			adopted   bool
		)
		if connectionEdit {
			verifyCfg = dialConfig(id, next, secrets)
			// The candidate's transport shape is checked against the
			// operator's words before any authority is exercised: a
			// field belonging to the other transport is a request
			// defect, not a connection failure.
			if msg := configProblem(verifyCfg); msg != "" {
				return errors.Join(
					api.WriteError(w, http.StatusBadRequest, msg), withdraw())
			}
			client, err := verify(r.Context(), verifyCfg)
			if err != nil {
				return errors.Join(
					api.WriteError(w, http.StatusBadRequest, "Connection check failed: "+err.Error()),
					withdraw())
			}
			verified = client
			defer func() {
				if !adopted {
					_ = verified.Close()
				}
			}()
		}

		before := toolIDs(r.Context(), queries, id)
		if err := writeRow(); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return errors.Join(err, withdraw())
		}
		toggleAudit, err := toggle()
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return err
		}

		saved, err := queries.GetMcpServer(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read back mcp server %q: %w", id, err)
		}
		discoverCtx, cancel := requestContext(r.Context())
		defer cancel()
		var (
			listed  []mcp.ListedTool
			skipped []mcp.Skipped
		)
		switch {
		case saved.Enabled == 0:
			// Disabled: the stored listing, no dial.
			if listed, err = catalogListing(r.Context(), queries, id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
		case verified != nil:
			// One publish for definition, implementation and authority:
			// the catalog write commits first, and only a committed
			// generation installs the verified client. A failed
			// publication leaves the manager untouched — the previous
			// generation keeps serving — and ownership stays here, so
			// the deferred close reclaims the candidate.
			listed, skipped, err = mcpserver.PublishVerified(discoverCtx, sqlDB, vaultStore, manager, saved, verified)
			adopted = err == nil
			if err != nil {
				// A failed publication leaves the row explaining itself,
				// exactly like a refresh: diagnostics land, and a rejected
				// generation would pause the catalog until a valid listing
				// arrives (RecordPublishFailure decides).
				_ = mcpserver.RecordPublishFailure(r.Context(), queries, id, err)
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("discover mcp server %q: %w", id, err)
			}
			// An endpoint, credential or process change retires the
			// admitted work: the old connection drains without aborting
			// it, so its calls are revoked explicitly. Headers and env
			// are write-only, so any resubmission counts as a credential
			// change even when the map equals the stored one. A rename or
			// a policy widening leaves them running — same endpoint, same
			// credentials.
			endpointMoved := urlMoved || transportChanged || req.Command != nil || req.Args != nil
			if (endpointMoved || req.Headers != nil || req.Env != nil) && canceler != nil {
				canceler.CancelServerTools(id)
			}
		case enabling:
			// A re-enable reconnects through the manager: its failure
			// degrades to the stored listing with diagnostics recorded.
			if listed, skipped, err = mcpserver.Discover(discoverCtx, sqlDB, queries, manager, vaultStore, logger, saved); err != nil {
				if listed, err = catalogListing(r.Context(), queries, id); err != nil {
					_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
					return err
				}
				skipped = nil
			}
		default:
			// A rename skips discovery; the response still reports the
			// stored listing.
			if listed, err = catalogListing(r.Context(), queries, id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
		}

		// Only tools the publish dropped lose their in-flight calls.
		cancelRemovedTools(canceler, id, before, keptFromListing(id, listed))
		// Authority removals are recorded after the tightening persisted —
		// a URL move's revocation included: a failed verify leaves the old
		// endpoint's approvals in force, so the trail must not claim
		// otherwise.
		writeErr := respond(http.StatusOK, listed, skipped)
		return errors.Join(writeErr, toggleAudit,
			revokeAuthority(r.Context(), queries, id,
				privateRemoved || (urlMoved && row.AllowPrivateNetwork != 0),
				plainRemoved || (urlMoved && row.AllowPlaintext != 0)))
	}
}

// catalogListing renders the server's stored catalog rows in the
// listing shape discovery would have returned.
func catalogListing(ctx context.Context, queries *db.Queries, serverID string) ([]mcp.ListedTool, error) {
	rows, err := queries.ListMcpServerTools(ctx, sql.NullString{String: serverID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list tools for %q: %w", serverID, err)
	}
	out := make([]mcp.ListedTool, 0, len(rows))
	for _, row := range rows {
		out = append(out, mcp.ListedTool{
			Remote:         row.Name,
			Description:    row.Description,
			DefinitionJSON: row.DefinitionJson,
		})
	}
	return out, nil
}

// serverPatch is the next row state, shaped for UpdateMcpServer.
type serverPatch struct {
	name                string
	transport           string
	url                 sql.NullString
	command             string
	args                []string
	inheritEnv          bool
	timeout             sql.NullInt64
	allowPrivateNetwork bool
	allowPlaintext      bool
	connectionChanged   bool
}

// applyPatch folds a request into the row. Nil maps and slices keep
// stored values; non-nil ones replace. A present field that equals
// the stored value is not a connection change — a no-op patch stays a
// cheap rename. A transport change folds the old transport's fields away:
// a stdio server has no endpoint or destination approvals, an http server
// no process shape.
func applyPatch(row db.McpServer, req apitypes.UpdateMcpServerRequest) (serverPatch, string) {
	args, err := mcpserver.DecodeStrings(row.Args)
	if err != nil {
		return serverPatch{}, fmt.Sprintf("stored args for %q are unreadable", row.ID)
	}
	next := serverPatch{
		name:                row.Name,
		transport:           row.Transport,
		url:                 row.Url,
		command:             row.Command.String,
		args:                args,
		inheritEnv:          row.InheritEnv != 0,
		timeout:             row.TimeoutMs,
		allowPrivateNetwork: row.AllowPrivateNetwork != 0,
		allowPlaintext:      row.AllowPlaintext != 0,
	}

	if req.Name != nil {
		if !names.Valid(*req.Name) {
			return next, "Invalid server name: use letters, digits and single spaces only"
		}
		next.name = strings.TrimSpace(*req.Name)
	}
	if req.Transport != nil && *req.Transport != next.transport {
		next.transport = *req.Transport
		next.connectionChanged = true
		if next.transport == mcp.TransportStdio {
			next.url = sql.NullString{}
			next.allowPrivateNetwork = false
			next.allowPlaintext = false
		} else {
			next.command = ""
			next.args = nil
			next.inheritEnv = false
		}
	}
	if req.Command != nil {
		command := strings.TrimSpace(*req.Command)
		if command != next.command {
			next.connectionChanged = true
		}
		next.command = command
	}
	if req.Args != nil {
		if !slices.Equal(req.Args, next.args) {
			next.connectionChanged = true
		}
		next.args = req.Args
	}
	if req.InheritEnv != nil {
		if *req.InheritEnv != next.inheritEnv {
			next.connectionChanged = true
		}
		next.inheritEnv = *req.InheritEnv
	}
	if req.URL != nil {
		v := strings.TrimSpace(*req.URL)
		next.url = sql.NullString{String: v, Valid: v != ""}
		if !sameNullString(next.url, row.Url) {
			// A destination change invalidates its approvals: the new
			// endpoint was never approved for private access or
			// plaintext. The same patch may re-approve below.
			next.connectionChanged = true
			next.allowPrivateNetwork = false
			next.allowPlaintext = false
		}
	}
	if req.AllowPrivateNetwork != nil {
		next.allowPrivateNetwork = *req.AllowPrivateNetwork
		if next.allowPrivateNetwork != (row.AllowPrivateNetwork != 0) {
			next.connectionChanged = true
		}
	}
	if req.AllowPlaintext != nil {
		next.allowPlaintext = *req.AllowPlaintext
		if next.allowPlaintext != (row.AllowPlaintext != 0) {
			next.connectionChanged = true
		}
	}
	if req.ClearTimeoutMs && row.TimeoutMs.Valid {
		next.timeout = sql.NullInt64{}
		next.connectionChanged = true
	}
	if req.TimeoutMs != nil {
		if msg := timeoutProblem(req.TimeoutMs); msg != "" {
			return next, msg
		}
		next.timeout = sql.NullInt64{Int64: *req.TimeoutMs, Valid: true}
		if !sameNullInt64(next.timeout, row.TimeoutMs) {
			next.connectionChanged = true
		}
	}
	return next, ""
}

// sameNullString reports whether two nullable columns hold the same
// value (absent matches absent).
func sameNullString(a, b sql.NullString) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.String == b.String
}

// sameNullInt64 is sameNullString's integer twin.
func sameNullInt64(a, b sql.NullInt64) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Int64 == b.Int64
}

// dialConfig builds the verification dial for the next row state: the
// candidate carries the transport shape, the secrets the operation wrote,
// and the policy flags, so verification, discovery and refresh are denied
// exactly where the runtime is.
func dialConfig(id string, next serverPatch, secrets mcpserver.Secrets) mcp.ServerConfig {
	cfg := mcp.ServerConfig{
		ID:                  id,
		Transport:           next.transport,
		URL:                 next.url.String,
		Command:             next.command,
		Args:                next.args,
		Env:                 secrets.Env,
		InheritEnv:          next.inheritEnv,
		Headers:             secrets.Headers,
		AllowPrivateNetwork: next.allowPrivateNetwork,
		AllowPlaintext:      next.allowPlaintext,
	}
	if next.timeout.Valid && next.timeout.Int64 > 0 {
		cfg.Timeout = msDuration(next.timeout.Int64)
	}
	return cfg
}
