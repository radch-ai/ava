// PATCH /api/mcp/servers/{id} - edit a server with absent-keeps
// semantics: a present env/headers map replaces the stored one
// wholesale, an absent field keeps it. Connection-affecting edits are
// re-verified before anything is written; a rename touches neither the
// vault nor the server process.
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

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/names"
	"github.com/radch-ai/ava/backend/internal/vault"
)

// Update applies one patch. Renames without connection changes skip the
// re-verify, the vault, and discovery; anything that changes what a dial
// looks like proves the new config connects before a single write
// happens.
func Update(throttle *api.Throttle, manager *mcp.Manager, sqlDB *sql.DB, queries *db.Queries, vaultStore *vault.Store) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.UpdateMcpServerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")

		row, err := readServer(r.Context(), queries, w, id)
		if err != nil {
			return err
		}
		if !throttle.Allow("mcp:" + id) {
			return api.WriteError(w, http.StatusTooManyRequests,
				"Editing too fast — try again in a moment")
		}

		rowArgs, err := decodeStrings(row.Args)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("decode stored args for %q: %w", id, err)
		}
		next, problem := applyPatch(row, rowArgs, req)
		if problem != "" {
			return api.WriteError(w, http.StatusBadRequest, problem)
		}
		// Effective secrets change a dial too: replaced maps are a
		// connection-affecting edit even when no config field moved.
		connectionEdit := next.connectionChanged || req.Env != nil || req.Headers != nil

		var secrets, storedSecrets mcpserver.Secrets
		secretsReplaced := false
		if connectionEdit {
			if secrets, err = unsealSecrets(r.Context(), vaultStore, id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("unseal secrets for %q: %w", id, err)
			}
			// The stored values ride along in case the row write below
			// fails after a successful re-seal and must be restored.
			storedSecrets = secrets
			if req.Env != nil {
				secrets.Env = req.Env
				secretsReplaced = true
			}
			if req.Headers != nil {
				secrets.Headers = req.Headers
				secretsReplaced = true
			}
		}

		// The candidate config dials with the effective secrets; the
		// verified client is adopted after the writes so discovery reuses
		// it instead of spawning a second process.
		var (
			verified  *mcp.Client
			verifyCfg mcp.ServerConfig
			adopted   bool
		)
		if connectionEdit {
			verifyCfg = dialConfig(id, next, secrets)
			client, _, err := verify(r.Context(), verifyCfg)
			if err != nil {
				return api.WriteError(w, http.StatusBadRequest,
					"Connection check failed: "+err.Error())
			}
			verified = client
			defer func() {
				if !adopted {
					_ = verified.Close()
				}
			}()
		}

		// Key-name columns follow the effective secrets; a rename passes
		// the stored names through untouched.
		envKeys, headerKeys := row.EnvKeys, row.HeaderKeys
		if connectionEdit {
			envKeys, headerKeys = encodeKeyNames(secrets.Env), encodeKeyNames(secrets.Headers)
		}
		// A pure switch patch writes no config: enabled lives on its own
		// query (mcp_servers.sql), and the row is otherwise untouched.
		// Replaced secret maps do write: their key-name columns change.
		configTouched := secretsReplaced || req.Name != nil || req.Transport != nil ||
			req.Command != nil || req.Args != nil || req.URL != nil ||
			req.TimeoutMs != nil || req.ClearTimeoutMs
		var argsJSON []byte
		if configTouched {
			if argsJSON, err = json.Marshal(next.args); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("encode args for %q: %w", id, err)
			}
		}
		// The blob is re-sealed before the row is written — the row is
		// the commit point — with the kept map riding along unchanged:
		// a failure between the two writes must never leave key-name
		// columns promising values the vault lacks. If the row write
		// then fails, the stored values go back.
		if secretsReplaced {
			if err := sealSecrets(r.Context(), vaultStore, id, secrets); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("seal secrets for %q: %w", id, err)
			}
		}
		if configTouched {
			if err := queries.UpdateMcpServer(r.Context(), db.UpdateMcpServerParams{
				Name:       next.name,
				Transport:  next.transport,
				Command:    next.command,
				Args:       string(argsJSON),
				Url:        next.url,
				EnvKeys:    envKeys,
				HeaderKeys: headerKeys,
				TimeoutMs:  next.timeout,
				ID:         id,
			}); err != nil {
				var rollback error
				if secretsReplaced {
					if rbErr := sealSecrets(r.Context(), vaultStore, id, storedSecrets); rbErr != nil {
						rollback = fmt.Errorf("restore sealed secrets for %q: %w", id, rbErr)
					}
				}
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return errors.Join(fmt.Errorf("update mcp server %q: %w", id, err), rollback)
			}
		}
		if verified != nil {
			manager.Adopt(verifyCfg, verified)
			adopted = true
		}

		// The global switch is a pure state flip. Disabling
		// also tears the live client down -- no process serves a blocked
		// server; the catalog rows stay, so re-enabling reconnects
		// without a reinstall.
		if req.Enabled != nil {
			enabled := int64(0)
			if *req.Enabled {
				enabled = 1
			}
			if _, err := queries.ToggleMcpServerEnabled(r.Context(), db.ToggleMcpServerEnabledParams{
				Enabled: enabled, ID: id,
			}); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return fmt.Errorf("toggle mcp server %q: %w", id, err)
			}
			if !*req.Enabled {
				manager.Close(id)
			}
		}

		saved, err := queries.GetMcpServer(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("read back mcp server %q: %w", id, err)
		}
		discoverCtx, cancel := requestContext(r.Context())
		defer cancel()
		var listed []mcp.ListedTool
		switch {
		case saved.Enabled == 0:
			// Disabled: the stored listing, no dial.
			if listed, err = catalogListing(r.Context(), queries, id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
		case connectionEdit || (req.Enabled != nil && *req.Enabled):
			// The verified client is adopted (a re-enable reconnects): one
			// discovery covers both. A connection edit's failure is a
			// server fault; a re-enable degrades to the stored listing with
			// diagnostics recorded.
			if listed, err = mcpserver.Discover(discoverCtx, sqlDB, queries, manager, vaultStore, saved); err != nil {
				if connectionEdit {
					_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
					return fmt.Errorf("discover mcp server %q: %w", id, err)
				}
				if listed, err = catalogListing(r.Context(), queries, id); err != nil {
					_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
					return err
				}
			}
		default:
			// A rename skips discovery; the response still reports the
			// stored listing.
			if listed, err = catalogListing(r.Context(), queries, id); err != nil {
				_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return err
			}
		}

		server, err := buildServer(r.Context(), queries, saved)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("build server %q: %w", id, err)
		}
		return api.WriteJSON(w, http.StatusOK, apitypes.McpServerWithToolsResponse{
			Server: server,
			Tools:  discoveredTools(id, listed),
		})
	}
}

// catalogListing renders the server's stored catalog rows in the
// listing shape discovery would have returned.
func catalogListing(ctx context.Context, queries *db.Queries, serverID string) ([]mcp.ListedTool, error) {
	rows, err := queries.ListMcpServerTools(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("list tools for %q: %w", serverID, err)
	}
	out := make([]mcp.ListedTool, 0, len(rows))
	for _, row := range rows {
		out = append(out, mcp.ListedTool{Remote: row.Name, Description: row.Description})
	}
	return out, nil
}

// serverPatch is the next row state, shaped for UpdateMcpServer.
type serverPatch struct {
	name              string
	transport         string
	command           sql.NullString
	args              []string
	url               sql.NullString
	timeout           sql.NullInt64
	connectionChanged bool
}

// applyPatch folds a request into the row. Nil maps and slices keep
// stored values; non-nil ones replace. A present field that equals
// the stored value is not a connection change — a no-op patch stays a
// cheap rename.
func applyPatch(row db.McpServer, rowArgs []string, req apitypes.UpdateMcpServerRequest) (serverPatch, string) {
	next := serverPatch{
		name:      row.Name,
		transport: row.Transport,
		command:   row.Command,
		args:      rowArgs,
		url:       row.Url,
		timeout:   row.TimeoutMs,
	}

	if req.Name != nil {
		if !names.Valid(*req.Name) {
			return next, "Invalid server name: use letters, digits and single spaces only"
		}
		next.name = strings.TrimSpace(*req.Name)
	}
	if req.Transport != nil {
		if *req.Transport != mcp.TransportStdio && *req.Transport != mcp.TransportHTTP {
			return next, `transport must be "stdio" or "http"`
		}
		if *req.Transport != row.Transport {
			next.transport = *req.Transport
			next.connectionChanged = true
		}
	}
	if req.Command != nil {
		v := strings.TrimSpace(*req.Command)
		next.command = sql.NullString{String: v, Valid: v != ""}
		if !sameNullString(next.command, row.Command) {
			next.connectionChanged = true
		}
	}
	if req.Args != nil {
		if !slices.Equal(req.Args, rowArgs) {
			next.args = req.Args
			next.connectionChanged = true
		}
	}
	if req.URL != nil {
		v := strings.TrimSpace(*req.URL)
		next.url = sql.NullString{String: v, Valid: v != ""}
		if !sameNullString(next.url, row.Url) {
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

	// Cross-field validity on the next state: a transport switch needs
	// the new transport's address field in the same patch.
	command, rawURL := "", ""
	if next.command.Valid {
		command = next.command.String
	}
	if next.url.Valid {
		rawURL = next.url.String
	}
	if msg := configProblem(next.transport, command, rawURL); msg != "" {
		return next, msg
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

// unsealSecrets reads the stored secrets blob; a missing entry is an
// honestly empty one.
func unsealSecrets(ctx context.Context, vaultStore *vault.Store, slug string) (mcpserver.Secrets, error) {
	var secrets mcpserver.Secrets
	blob, _, err := vaultStore.Get(ctx, vault.NameRuntime, mcpserver.EntryName(slug))
	if errors.Is(err, vault.ErrNotFound) {
		return secrets, nil
	}
	if err != nil {
		return secrets, err
	}
	if err := json.Unmarshal(blob, &secrets); err != nil {
		return secrets, fmt.Errorf("decode secrets: %w", err)
	}
	return secrets, nil
}

// dialConfig builds the verification dial for the next row state.
func dialConfig(id string, next serverPatch, secrets mcpserver.Secrets) mcp.ServerConfig {
	cfg := mcp.ServerConfig{
		ID:        id,
		Transport: next.transport,
		Env:       secrets.Env,
		Headers:   secrets.Headers,
	}
	if next.command.Valid {
		cfg.Command = next.command.String
	}
	if next.url.Valid {
		cfg.URL = next.url.String
	}
	cfg.Args = next.args
	if next.timeout.Valid && next.timeout.Int64 > 0 {
		cfg.Timeout = msDuration(next.timeout.Int64)
	}
	return cfg
}
