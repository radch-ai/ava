package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tier constants represent the subscription tiers available to users.
const (
	TierFree = iota
	TierPro
	TierEnterprise
)

// QuotaManager manages credit quotas and usage tracking for users.
// It provides methods to check quotas, reserve credits, and deduct credits
// after LLM operations complete.
//
// This is a stub implementation (Wave 4). The full implementation will include:
// - Period-based rate limiting with 6-hour windows
// - Redis caching for performance
// - Credit reservation and deduction
// - Tier-based limits (agent count, cron job count)
type QuotaManager struct {
	db *pgxpool.Pool
}

// NewQuotaManager creates a new QuotaManager with the given database connection pool.
func NewQuotaManager(db *pgxpool.Pool) *QuotaManager {
	return &QuotaManager{
		db: db,
	}
}

// CheckQuota verifies that a user has sufficient credits to perform an operation.
// It checks both the user's total credit balance and the current period's usage limit.
//
// TODO (Wave 4): Implement full quota checking logic:
// - Query user's credits_remaining from users table
// - Calculate current 6-hour period
// - Check period usage against limit (with 10% buffer, capped at remaining credits)
// - Use Redis cache for period limits and usage
// - Return error if insufficient credits or period limit exceeded
func (qm *QuotaManager) CheckQuota(ctx context.Context, userID string, creditsNeeded float64) error {
	// TODO: Implement credit balance check
	// TODO: Implement period limit check with 10% buffer
	// TODO: Add Redis caching for performance
	return fmt.Errorf("CheckQuota not yet implemented (Wave 4)")
}

// ReserveCredit reserves credits for an operation before it executes.
// This ensures credits are available when the operation starts.
//
// TODO (Wave 4): Implement credit reservation:
// - Atomically reserve credits from user's balance
// - Track reservation in Redis or database
// - Handle concurrent requests safely
// - Support refund of unused credits
func (qm *QuotaManager) ReserveCredit(ctx context.Context, userID string, amount float64) error {
	// TODO: Implement atomic credit reservation
	// TODO: Add refund mechanism for unused reservations
	return fmt.Errorf("ReserveCredit not yet implemented (Wave 4)")
}

// DeductCredit deducts credits from a user's balance after an LLM operation completes.
// It also logs the usage for billing and analytics purposes.
//
// TODO (Wave 4): Implement credit deduction:
// - Calculate actual credit cost based on tokens used
// - Deduct from users.credits_remaining
// - Insert record into usage_logs table
// - Update Redis cache for period usage
// - Handle any reservation cleanup
func (qm *QuotaManager) DeductCredit(ctx context.Context, userID string, agentID string, tokensUsed int, creditCost float64) error {
	// TODO: Implement credit deduction from database
	// TODO: Insert usage log record
	// TODO: Update period usage in Redis
	return fmt.Errorf("DeductCredit not yet implemented (Wave 4)")
}
