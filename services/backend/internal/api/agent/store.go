// Package agent provides API-layer agent configuration storage operations.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nevaria-ai/backend/internal/api/shared"
	"github.com/nevaria-ai/backend/internal/db"
)

type Store interface {
	// GetAgentConfiguration retrieves the full configuration for an agent.
	GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error)
	GetAgentActivityRuns(ctx context.Context, userID, agentName string, params ActivityParams) ([]ActivityItem, error)
	// PatchAgentConfiguration performs a partial update of agent configuration in a transaction.
	PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error)
	// CreateAgent creates a new agent with automatic name suffixing.
	CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error)
}

type ActivityParams struct {
	Limit  int
	Cursor *shared.Cursor
}

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{
		pool:    pool,
		queries: db.New(pool),
	}
}

// GetAgentConfiguration retrieves the full configuration for an agent.
func (s *PGStore) GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error) {
	uid := shared.ParseUserID(userID)

	row, err := s.queries.GetAgentConfiguration(ctx, db.GetAgentConfigurationParams{
		UserID: uid,
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

	if row.Icon.Valid {
		config.Icon = row.Icon.String
	}

	if row.Soul != nil {
		soulBytes, err := json.Marshal(row.Soul)
		if err != nil {
			return nil, fmt.Errorf("marshal soul: %w", err)
		}
		if err := json.Unmarshal(soulBytes, &config.Soul); err != nil {
			return nil, fmt.Errorf("parse soul: %w", err)
		}
	}

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

	if row.ToolsEnabled != nil {
		toolsBytes, err := json.Marshal(row.ToolsEnabled)
		if err != nil {
			return nil, fmt.Errorf("marshal tools enabled: %w", err)
		}
		if err := json.Unmarshal(toolsBytes, &config.ToolsEnabled); err != nil {
			return nil, fmt.Errorf("parse tools enabled: %w", err)
		}
	}

	if len(row.Heartbeat) > 0 {
		var hb HeartbeatConfig
		if err := json.Unmarshal(row.Heartbeat, &hb); err != nil {
			return nil, fmt.Errorf("parse heartbeat: %w", err)
		}
		config.Heartbeat = &hb
	}

	if len(row.CronJobs) > 0 {
		if err := json.Unmarshal(row.CronJobs, &config.CronJobs); err != nil {
			return nil, fmt.Errorf("parse cron jobs: %w", err)
		}
	}

	return config, nil
}

func (s *PGStore) GetAgentActivityRuns(ctx context.Context, userID, agentName string, params ActivityParams) ([]ActivityItem, error) {
	parsedUserID := shared.ParseUserID(userID)

	if params.Cursor == nil {
		rows, err := s.queries.ListAgentRunsFirstPage(ctx, db.ListAgentRunsFirstPageParams{
			UserID:    parsedUserID,
			AgentName: agentName,
			Limit:     int32(params.Limit + 1),
		})
		if err != nil {
			return nil, fmt.Errorf("query first activity page: %w", err)
		}
		return mapActivityRows(rows), nil
	}

	cursorID := shared.UUIDFromString(params.Cursor.ID)

	rows, err := s.queries.ListAgentRunsAfterCursor(ctx, db.ListAgentRunsAfterCursorParams{
		UserID:    parsedUserID,
		AgentName: agentName,
		CreatedAt: pgtype.Timestamptz{Time: params.Cursor.CreatedAt, Valid: true},
		ID:        cursorID,
		Limit:     int32(params.Limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("query cursor activity page: %w", err)
	}
	return mapActivityCursorRows(rows), nil
}

// mapActivityRows converts first page rows to activity items.
// Duplicate of mapActivityCursorRows is intentional - sqlc generates separate types for each query.
func mapActivityRows(rows []db.ListAgentRunsFirstPageRow) []ActivityItem {
	items := make([]ActivityItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ActivityItem{
			ID:          row.ID.String(),
			TriggeredBy: row.TriggeredBy,
			Action:      row.Action,
			Status:      row.Status,
			Message:     row.Message,
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return items
}

// mapActivityCursorRows converts cursor page rows to activity items.
// Duplicate of mapActivityRows is intentional - sqlc generates separate types for each query.
func mapActivityCursorRows(rows []db.ListAgentRunsAfterCursorRow) []ActivityItem {
	items := make([]ActivityItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ActivityItem{
			ID:          row.ID.String(),
			TriggeredBy: row.TriggeredBy,
			Action:      row.Action,
			Status:      row.Status,
			Message:     row.Message,
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return items
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
	uid := shared.ParseUserID(userID)

	if req.Icon != nil {
		if err := txQueries.UpdateAgentIcon(ctx, db.UpdateAgentIconParams{
			Icon:   shared.PgText(*req.Icon),
			UserID: uid,
			Name:   agentName,
		}); err != nil {
			return nil, fmt.Errorf("update icon: %w", err)
		}
	}

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
			UserID:  uid,
			Name:    agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("update config: %w", err)
		}
	}

	if req.ToolsEnabled != nil {
		err = txQueries.ClearAgentTools(ctx, db.ClearAgentToolsParams{
			UserID:    uid,
			AgentName: agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("clear agent tools: %w", err)
		}

		for _, toolID := range req.ToolsEnabled {
			err = txQueries.InsertAgentTool(ctx, db.InsertAgentToolParams{
				UserID:    uid,
				AgentName: agentName,
				Column3:   shared.UUIDFromString(toolID),
			})
			if err != nil {
				return nil, fmt.Errorf("insert agent tool %s: %w", toolID, err)
			}
		}
	}

	if req.Heartbeat != nil {
		err = txQueries.UpsertHeartbeat(ctx, db.UpsertHeartbeatParams{
			UserID:    uid,
			AgentName: agentName,
			Model:     req.Heartbeat.Model,
			Enabled:   shared.PgBool(req.Heartbeat.Enabled),
			Duration:  req.Heartbeat.Duration,
			Content:   req.Heartbeat.Content,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert heartbeat: %w", err)
		}
	}

	if req.CronJobs != nil {
		err = txQueries.DeleteCronJobs(ctx, db.DeleteCronJobsParams{
			UserID:    uid,
			AgentName: agentName,
		})
		if err != nil {
			return nil, fmt.Errorf("delete cron jobs: %w", err)
		}

		for _, job := range req.CronJobs {
			err = txQueries.InsertCronJob(ctx, db.InsertCronJobParams{
				UserID:         uid,
				AgentName:      agentName,
				Time:           job.Time,
				Frequency:      job.Frequency,
				CronExpression: shared.PgText(job.CronExpression),
				Prompt:         job.Prompt,
			})
			if err != nil {
				return nil, fmt.Errorf("insert cron job: %w", err)
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return s.GetAgentConfiguration(ctx, userID, agentName)
}

// maxSuffixLimit is the maximum suffix number to try before giving up.
const maxSuffixLimit = 1000

// CreateAgent creates a new agent with automatic name suffixing.
func (s *PGStore) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	uid := shared.ParseUserID(userID)
	baseName := req.Name

	existingNames, err := s.queries.CheckAgentNameAvailability(ctx, db.CheckAgentNameAvailabilityParams{
		UserID: uid,
		Name:   baseName,
	})
	if err != nil {
		return nil, fmt.Errorf("check name availability: %w", err)
	}

	if len(existingNames) == 0 {
		return s.createAgentRow(ctx, uid, baseName, req)
	}

	for _, name := range existingNames {
		if name == baseName {
			finalName, err := s.findAvailableName(existingNames, baseName)
			if err != nil {
				return nil, fmt.Errorf("find available name: %w", err)
			}
			return s.createAgentRow(ctx, uid, finalName, req)
		}
	}

	return s.createAgentRow(ctx, uid, baseName, req)
}

func (s *PGStore) findAvailableName(existingNames []string, baseName string) (string, error) {
	existingSet := make(map[string]bool)
	for _, n := range existingNames {
		existingSet[n] = true
	}

	for suffix := 2; suffix <= maxSuffixLimit; suffix++ {
		candidate := fmt.Sprintf("%s-%d", baseName, suffix)
		if !existingSet[candidate] {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no available name found after %d suffixes", maxSuffixLimit)
}

func (s *PGStore) createAgentRow(ctx context.Context, userID int64, name string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
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

	if err := txQueries.CreateAgent(ctx, db.CreateAgentParams{
		UserID:  userID,
		Name:    name,
		Icon:    shared.PgText(req.Icon),
		Column4: configJSON,
	}); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	if req.Heartbeat != nil {
		if err := txQueries.UpsertHeartbeat(ctx, db.UpsertHeartbeatParams{
			UserID:    userID,
			AgentName: name,
			Model:     req.Heartbeat.Model,
			Enabled:   shared.PgBool(req.Heartbeat.Enabled),
			Duration:  req.Heartbeat.Duration,
			Content:   req.Heartbeat.Content,
		}); err != nil {
			return nil, fmt.Errorf("create heartbeat: %w", err)
		}
	}

	if req.ToolsEnabled != nil {
		for _, toolID := range req.ToolsEnabled {
			if err := txQueries.InsertAgentTool(ctx, db.InsertAgentToolParams{
				UserID:    userID,
				AgentName: name,
				Column3:   shared.UUIDFromString(toolID),
			}); err != nil {
				return nil, fmt.Errorf("insert tool %s: %w", toolID, err)
			}
		}
	}

	if req.CronJobs != nil {
		for _, job := range req.CronJobs {
			if err := txQueries.InsertCronJob(ctx, db.InsertCronJobParams{
				UserID:         userID,
				AgentName:      name,
				Time:           job.Time,
				Frequency:      job.Frequency,
				CronExpression: shared.PgText(job.CronExpression),
				Prompt:         job.Prompt,
			}); err != nil {
				return nil, fmt.Errorf("insert cron job: %w", err)
			}
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &CreateAgentResponse{Name: name}, nil
}
