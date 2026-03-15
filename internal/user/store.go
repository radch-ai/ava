// Package user provides user-related data storage.
package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nevaria-ai/backend/internal/db"
)

// Store defines the interface for user data storage operations.
type Store interface {
	// GetAgentIDsByUserID retrieves all agent IDs for a given user.
	GetAgentIDsByUserID(ctx context.Context, userID string) ([]string, error)
	// GetAgentConfiguration retrieves the full configuration for an agent.
	GetAgentConfiguration(ctx context.Context, agentID string) (*AgentConfiguration, error)
	// PatchAgentConfiguration performs a partial update of agent configuration in a transaction.
	PatchAgentConfiguration(ctx context.Context, agentID string, req *PatchAgentRequest) (*AgentConfiguration, error)
}

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPGStore creates a new PostgreSQL store.
func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{
		pool:    pool,
		queries: db.New(pool),
	}
}

// uuidFromString converts a string UUID to pgtype.UUID.
func uuidFromString(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

// uuidsToStrings converts pgtype.UUID slice to string slice.
func uuidsToStrings(uuids []pgtype.UUID) []string {
	result := make([]string, 0, len(uuids))
	for _, u := range uuids {
		if u.Valid {
			result = append(result, u.String())
		}
	}
	return result
}

// GetAgentIDsByUserID retrieves all agent IDs for a given user.
func (s *PGStore) GetAgentIDsByUserID(ctx context.Context, userID string) ([]string, error) {
	uuid := uuidFromString(userID)

	agentUUIDs, err := s.queries.GetAgentIDsByUserID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("query agent IDs: %w", err)
	}

	if len(agentUUIDs) == 0 {
		return []string{}, nil
	}

	return uuidsToStrings(agentUUIDs), nil
}

// GetAgentConfiguration retrieves the full configuration for an agent.
func (s *PGStore) GetAgentConfiguration(ctx context.Context, agentID string) (*AgentConfiguration, error) {
	uuid := uuidFromString(agentID)

	row, err := s.queries.GetAgentConfiguration(ctx, uuid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("agent not found: %s", agentID)
		}
		return nil, fmt.Errorf("query agent configuration: %w", err)
	}

	config := &AgentConfiguration{}

	// Parse soul
	if row.Soul != nil {
		soulBytes, err := json.Marshal(row.Soul)
		if err != nil {
			return nil, fmt.Errorf("marshal soul: %w", err)
		}
		if err := json.Unmarshal(soulBytes, &config.Soul); err != nil {
			return nil, fmt.Errorf("parse soul: %w", err)
		}
	}

	// Set string fields
	if row.UserContext != nil {
		if str, ok := row.UserContext.(string); ok {
			config.UserContext = str
		}
	}
	if row.OperatingInstructions != nil {
		if str, ok := row.OperatingInstructions.(string); ok {
			config.OperatingInstructions = str
		}
	}
	if row.ToolsContext != nil {
		if str, ok := row.ToolsContext.(string); ok {
			config.ToolsContext = str
		}
	}

	// Parse tools enabled
	if row.ToolsEnabled != nil {
		toolsBytes, err := json.Marshal(row.ToolsEnabled)
		if err != nil {
			return nil, fmt.Errorf("marshal tools enabled: %w", err)
		}
		if err := json.Unmarshal(toolsBytes, &config.ToolsEnabled); err != nil {
			return nil, fmt.Errorf("parse tools enabled: %w", err)
		}
	}

	// Parse heartbeat
	if row.Heartbeat != nil {
		var hb HeartbeatConfig
		if err := json.Unmarshal(row.Heartbeat, &hb); err != nil {
			return nil, fmt.Errorf("parse heartbeat: %w", err)
		}
		config.Heartbeat = &hb
	}

	// Parse cron jobs
	if row.CronJobs != nil {
		if err := json.Unmarshal(row.CronJobs, &config.CronJobs); err != nil {
			return nil, fmt.Errorf("parse cron jobs: %w", err)
		}
	}

	return config, nil
}

// PatchAgentConfiguration performs a partial update of agent configuration in a transaction.
func (s *PGStore) PatchAgentConfiguration(ctx context.Context, agentID string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Get transaction-scoped queries
	txQueries := s.queries.WithTx(tx)
	agentUUID := uuidFromString(agentID)

	// 1. Update config JSONB (if any config fields provided)
	if req.Soul != nil || req.UserContext != nil || req.OperatingInstructions != nil || req.ToolsContext != nil {
		configUpdates := make(map[string]interface{})
		if req.Soul != nil {
			configUpdates["soul"] = req.Soul
		}
		if req.UserContext != nil {
			configUpdates["userContext"] = *req.UserContext
		}
		if req.OperatingInstructions != nil {
			configUpdates["operatingInstructions"] = *req.OperatingInstructions
		}
		if req.ToolsContext != nil {
			configUpdates["toolsContext"] = *req.ToolsContext
		}

		configJSON, marshalErr := json.Marshal(configUpdates)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal config updates: %w", marshalErr)
		}

		err = txQueries.UpdateAgentConfig(ctx, db.UpdateAgentConfigParams{
			Column1: configJSON,
			AgentID: agentUUID,
		})
		if err != nil {
			return nil, fmt.Errorf("update config: %w", err)
		}
	}

	// 2. Sync agent_tools junction table
	if req.ToolsEnabled != nil {
		err = txQueries.DeleteAgentTools(ctx, agentUUID)
		if err != nil {
			return nil, fmt.Errorf("delete agent tools: %w", err)
		}

		for _, toolID := range req.ToolsEnabled {
			err = txQueries.InsertAgentTool(ctx, db.InsertAgentToolParams{
				AgentID: agentUUID,
				Column2: uuidFromString(toolID),
			})
			if err != nil {
				return nil, fmt.Errorf("insert agent tool %s: %w", toolID, err)
			}
		}
	}

	// 3. Upsert heartbeat
	if req.Heartbeat != nil {
		enabled := pgtype.Bool{Bool: req.Heartbeat.Enabled, Valid: true}
		err = txQueries.UpsertHeartbeat(ctx, db.UpsertHeartbeatParams{
			AgentID:  agentUUID,
			Model:    req.Heartbeat.Model,
			Enabled:  enabled,
			Duration: req.Heartbeat.Duration,
			Content:  req.Heartbeat.Content,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert heartbeat: %w", err)
		}
	}

	// 4. Replace cron jobs (full replace)
	if req.CronJobs != nil {
		err = txQueries.DeleteCronJobs(ctx, agentUUID)
		if err != nil {
			return nil, fmt.Errorf("delete cron jobs: %w", err)
		}

		for _, job := range req.CronJobs {
			cronExpr := pgtype.Text{String: job.CronExpression, Valid: job.CronExpression != ""}
			err = txQueries.InsertCronJob(ctx, db.InsertCronJobParams{
				AgentID:        agentUUID,
				Time:           job.Time,
				Frequency:      job.Frequency,
				CronExpression: cronExpr,
				Prompt:         job.Prompt,
			})
			if err != nil {
				return nil, fmt.Errorf("insert cron job: %w", err)
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Return updated configuration using GetAgentConfiguration
	return s.GetAgentConfiguration(ctx, agentID)
}
