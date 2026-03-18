// Package user provides agent configuration storage operations.
package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nevaria-ai/backend/internal/db"
)

// =============================================================================
// Agent Configuration Operations
// =============================================================================

// GetAgentConfiguration retrieves the full configuration for an agent.
func (s *PGStore) GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error) {
	userUUID := uuidFromString(userID)

	row, err := s.queries.GetAgentConfiguration(ctx, db.GetAgentConfigurationParams{
		UserID: userUUID,
		Name:   agentName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("agent not found: %s", agentName)
		}
		return nil, fmt.Errorf("query agent configuration: %w", err)
	}

	config := &AgentConfiguration{
		Name: row.Name,
	}

	// Set icon if valid
	if row.Icon.Valid {
		config.Icon = row.Icon.String
	}

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
	if len(row.Heartbeat) > 0 {
		var hb HeartbeatConfig
		if err := json.Unmarshal(row.Heartbeat, &hb); err != nil {
			return nil, fmt.Errorf("parse heartbeat: %w", err)
		}
		config.Heartbeat = &hb
	}

	// Parse cron jobs
	if len(row.CronJobs) > 0 {
		if err := json.Unmarshal(row.CronJobs, &config.CronJobs); err != nil {
			return nil, fmt.Errorf("parse cron jobs: %w", err)
		}
	}

	return config, nil
}

// PatchAgentConfiguration performs a partial update of agent configuration in a transaction.
func (s *PGStore) PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	txQueries := s.queries.WithTx(tx)
	userUUID := uuidFromString(userID)

	// 1. Update icon if provided
	if req.Icon != nil {
		if err := txQueries.UpdateAgentIcon(ctx, db.UpdateAgentIconParams{
			Icon:   pgText(*req.Icon),
			UserID: userUUID,
			Name:   agentName,
		}); err != nil {
			return nil, fmt.Errorf("update icon: %w", err)
		}
	}

	// 2. Update config JSONB (if any config fields provided)
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
			UserID:  userUUID,
			Name:    agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("update config: %w", err)
		}
	}

	// 3. Sync agent_tools junction table
	if req.ToolsEnabled != nil {
		err = txQueries.ClearAgentTools(ctx, db.ClearAgentToolsParams{
			UserID:    userUUID,
			AgentName: agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("clear agent tools: %w", err)
		}

		for _, toolID := range req.ToolsEnabled {
			err = txQueries.InsertAgentTool(ctx, db.InsertAgentToolParams{
				UserID:    userUUID,
				AgentName: agentName,
				Column3:   uuidFromString(toolID),
			})
			if err != nil {
				return nil, fmt.Errorf("insert agent tool %s: %w", toolID, err)
			}
		}
	}

	// 4. Upsert heartbeat
	if req.Heartbeat != nil {
		err = txQueries.UpsertHeartbeat(ctx, db.UpsertHeartbeatParams{
			UserID:    userUUID,
			AgentName: agentName,
			Model:     req.Heartbeat.Model,
			Enabled:   pgBool(req.Heartbeat.Enabled),
			Duration:  req.Heartbeat.Duration,
			Content:   req.Heartbeat.Content,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert heartbeat: %w", err)
		}
	}

	// 5. Replace cron jobs (full replace)
	if req.CronJobs != nil {
		err = txQueries.DeleteCronJobs(ctx, db.DeleteCronJobsParams{
			UserID:    userUUID,
			AgentName: agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("delete cron jobs: %w", err)
		}

		for _, job := range req.CronJobs {
			err = txQueries.InsertCronJob(ctx, db.InsertCronJobParams{
				UserID:         userUUID,
				AgentName:      agentName,
				Time:           job.Time,
				Frequency:      job.Frequency,
				CronExpression: pgText(job.CronExpression),
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

	// Return updated configuration
	return s.GetAgentConfiguration(ctx, userID, agentName)
}

// =============================================================================
// Agent Create Operations
// =============================================================================

// maxSuffixLimit is the maximum suffix number to try before giving up.
const maxSuffixLimit = 1000

// CreateAgent creates a new agent with automatic name suffixing.
func (s *PGStore) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	userUUID := uuidFromString(userID)
	baseName := req.Name

	// Check name availability - returns all names that match exact or with suffix
	existingNames, err := s.queries.CheckAgentNameAvailability(ctx, db.CheckAgentNameAvailabilityParams{
		UserID: userUUID,
		Name:   baseName,
	})
	if err != nil {
		return nil, fmt.Errorf("check name availability: %w", err)
	}

	// If no exact match, use the base name directly
	if len(existingNames) == 0 {
		return s.createAgentRow(ctx, userUUID, baseName, req)
	}

	// Check if exact name exists
	for _, name := range existingNames {
		if name == baseName {
			// Exact match exists, find available suffix
			finalName, err := s.findAvailableName(existingNames, baseName)
			if err != nil {
				return nil, fmt.Errorf("find available name: %w", err)
			}
			return s.createAgentRow(ctx, userUUID, finalName, req)
		}
	}

	// No exact match but suffixes exist - base name is available
	return s.createAgentRow(ctx, userUUID, baseName, req)
}

// findAvailableName finds the next available name with suffix (name-2, name-3, etc.).
func (s *PGStore) findAvailableName(existingNames []string, baseName string) (string, error) {
	// Build set of existing names for quick lookup
	existingSet := make(map[string]bool)
	for _, n := range existingNames {
		existingSet[n] = true
	}

	// Find next available suffix (start at 2, max at maxSuffixLimit)
	for suffix := 2; suffix <= maxSuffixLimit; suffix++ {
		candidate := fmt.Sprintf("%s-%d", baseName, suffix)
		if !existingSet[candidate] {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no available name found after %d suffixes", maxSuffixLimit)
}

// createAgentRow creates the agent row and associated data in a transaction.
func (s *PGStore) createAgentRow(ctx context.Context, userUUID pgtype.UUID, name string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	txQueries := s.queries.WithTx(tx)

	// Build config JSON
	configMap := make(map[string]interface{})
	if req.Soul != nil {
		configMap["soul"] = req.Soul
	}
	if req.UserContext != "" {
		configMap["userContext"] = req.UserContext
	}
	if req.OperatingInstructions != "" {
		configMap["operatingInstructions"] = req.OperatingInstructions
	}
	if req.ToolsContext != "" {
		configMap["toolsContext"] = req.ToolsContext
	}

	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	// Insert agent row
	if err := txQueries.CreateAgent(ctx, db.CreateAgentParams{
		UserID:  userUUID,
		Name:    name,
		Icon:    pgText(req.Icon),
		Column4: configJSON,
	}); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// Create heartbeat if provided
	if req.Heartbeat != nil {
		if err := txQueries.UpsertHeartbeat(ctx, db.UpsertHeartbeatParams{
			UserID:    userUUID,
			AgentName: name,
			Model:     req.Heartbeat.Model,
			Enabled:   pgBool(req.Heartbeat.Enabled),
			Duration:  req.Heartbeat.Duration,
			Content:   req.Heartbeat.Content,
		}); err != nil {
			return nil, fmt.Errorf("create heartbeat: %w", err)
		}
	}

	// Add tool associations
	if req.ToolsEnabled != nil {
		for _, toolID := range req.ToolsEnabled {
			if err := txQueries.InsertAgentTool(ctx, db.InsertAgentToolParams{
				UserID:    userUUID,
				AgentName: name,
				Column3:   uuidFromString(toolID),
			}); err != nil {
				return nil, fmt.Errorf("insert tool %s: %w", toolID, err)
			}
		}
	}

	// Create cron jobs
	if req.CronJobs != nil {
		for _, job := range req.CronJobs {
			if err := txQueries.InsertCronJob(ctx, db.InsertCronJobParams{
				UserID:         userUUID,
				AgentName:      name,
				Time:           job.Time,
				Frequency:      job.Frequency,
				CronExpression: pgText(job.CronExpression),
				Prompt:         job.Prompt,
			}); err != nil {
				return nil, fmt.Errorf("insert cron job: %w", err)
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &CreateAgentResponse{Name: name}, nil
}
