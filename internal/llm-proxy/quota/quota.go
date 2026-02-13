package quota

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// CheckResult represents the result of a quota check.
type CheckResult struct {
	HasQuota      bool    `json:"has_quota"`
	Source        string  `json:"source"` // "plan" or "addon"
	PlanPercent   int     `json:"plan_percent"`
	PlanDollars   float64 `json:"plan_dollars"`
	AddonDollars  float64 `json:"addon_dollars"`
	UsingAddon    bool    `json:"using_addon"`
	EstimatedCost float64 `json:"estimated_cost"`
}

// UserQuotaInfo represents quota information for a user.
type UserQuotaInfo struct {
	PlanPercent    int     `json:"plan_percent"`
	PlanDollars    float64 `json:"plan_dollars"`
	AddonDollars   float64 `json:"addon_dollars"`
	Reserved       float64 `json:"reserved"`
	DaysUntilReset int     `json:"days_until_reset"`
}

// CheckQuota verifies if a user has sufficient quota for an estimated cost.
// It checks plan quota first, then falls back to addon credits if needed.
func CheckQuota(ctx context.Context, client *redis.Client, userID string, estimatedCost float64) (*CheckResult, error) {
	// Fetch quota data from Redis (stored as integer cents/thousandths)
	planCents, err := client.Get(ctx, quotaKey(userID, "plan_dollars")).Result()
	if err == redis.Nil {
		// User has no quota set up
		return &CheckResult{HasQuota: false}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get plan quota: %w", err)
	}

	planCentsInt, _ := strconv.ParseInt(planCents, 10, 64)
	planDollarsFloat := centsToDollars(planCentsInt)

	addonCents, _ := client.Get(ctx, quotaKey(userID, "addon_dollars")).Result()
	addonCentsInt, _ := strconv.ParseInt(addonCents, 10, 64)
	addonDollarsFloat := centsToDollars(addonCentsInt)

	planPercent, _ := client.Get(ctx, quotaKey(userID, "plan_percent")).Result()
	planPercentInt, _ := strconv.Atoi(planPercent)

	// Check if plan has sufficient quota
	if planDollarsFloat >= estimatedCost {
		return &CheckResult{
			HasQuota:      true,
			Source:        "plan",
			PlanPercent:   planPercentInt,
			PlanDollars:   planDollarsFloat,
			AddonDollars:  addonDollarsFloat,
			UsingAddon:    false,
			EstimatedCost: estimatedCost,
		}, nil
	}

	// Plan insufficient, check addon
	if addonDollarsFloat >= estimatedCost {
		return &CheckResult{
			HasQuota:      true,
			Source:        "addon",
			PlanPercent:   planPercentInt,
			PlanDollars:   planDollarsFloat,
			AddonDollars:  addonDollarsFloat,
			UsingAddon:    true,
			EstimatedCost: estimatedCost,
		}, nil
	}

	// Neither plan nor addon has sufficient quota
	return &CheckResult{
		HasQuota:      false,
		Source:        "",
		PlanPercent:   planPercentInt,
		PlanDollars:   planDollarsFloat,
		AddonDollars:  addonDollarsFloat,
		UsingAddon:    false,
		EstimatedCost: estimatedCost,
	}, nil
}

// ReserveQuota reserves an estimated cost before an LLM call.
// It atomically deducts the amount from the user's balance and tracks the reservation.
func ReserveQuota(ctx context.Context, client *redis.Client, userID string, estimatedCost float64, source string) error {
	// Use a transaction to ensure atomicity
	pipe := client.Pipeline()

	// Decrement the balance
	balanceKey := quotaKey(userID, source+"_dollars")
	pipe.DecrBy(ctx, balanceKey, formatCents(estimatedCost))

	// Increment the reservation
	reserveKey := quotaKey(userID, "reserved")
	pipe.IncrBy(ctx, reserveKey, formatCents(estimatedCost))

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to reserve quota: %w", err)
	}

	return nil
}

// DeductQuota calculates the actual cost after a streaming response and adjusts the quota.
// If reserved > actual, it refunds the difference.
// If reserved < actual, it charges the additional amount.
func DeductQuota(ctx context.Context, client *redis.Client, userID string, reserved, actual float64, source string) error {
	diff := reserved - actual

	if diff > 0 {
		// Over-reserved, refund difference
		balanceKey := quotaKey(userID, source+"_dollars")
		err := client.IncrBy(ctx, balanceKey, formatCents(diff)).Err()
		if err != nil {
			return fmt.Errorf("failed to refund difference: %w", err)
		}
	} else if diff < 0 {
		// Under-reserved, charge more
		additionalCost := -diff
		balanceKey := quotaKey(userID, source+"_dollars")
		err := client.DecrBy(ctx, balanceKey, formatCents(additionalCost)).Err()
		if err != nil {
			return fmt.Errorf("failed to charge additional cost: %w", err)
		}
	}

	// Release the reservation (always release the full reserved amount)
	reserveKey := quotaKey(userID, "reserved")
	err := client.DecrBy(ctx, reserveKey, formatCents(reserved)).Err()
	if err != nil {
		return fmt.Errorf("failed to release reservation: %w", err)
	}

	return nil
}

// GetUserQuotaInfo retrieves quota information for a user.
func GetUserQuotaInfo(ctx context.Context, client *redis.Client, userID string) (*UserQuotaInfo, error) {
	// Fetch all quota fields in parallel
	pipe := client.Pipeline()

	planPercentCmd := client.Get(ctx, quotaKey(userID, "plan_percent"))
	planDollarsCmd := client.Get(ctx, quotaKey(userID, "plan_dollars"))
	addonDollarsCmd := client.Get(ctx, quotaKey(userID, "addon_dollars"))
	reservedCmd := client.Get(ctx, quotaKey(userID, "reserved"))
	daysCmd := client.Get(ctx, quotaKey(userID, "days_until_reset"))

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to fetch quota info: %w", err)
	}

	info := &UserQuotaInfo{}

	// Parse plan percent
	if planPercentCmd.Err() == nil {
		info.PlanPercent, _ = strconv.Atoi(planPercentCmd.Val())
	}

	// Parse plan dollars (stored as integer cents)
	if planDollarsCmd.Err() == nil {
		cents, _ := strconv.ParseInt(planDollarsCmd.Val(), 10, 64)
		info.PlanDollars = centsToDollars(cents)
	}

	// Parse addon dollars (stored as integer cents)
	if addonDollarsCmd.Err() == nil {
		cents, _ := strconv.ParseInt(addonDollarsCmd.Val(), 10, 64)
		info.AddonDollars = centsToDollars(cents)
	}

	// Parse reserved (stored as integer cents)
	if reservedCmd.Err() == nil {
		cents, _ := strconv.ParseInt(reservedCmd.Val(), 10, 64)
		info.Reserved = centsToDollars(cents)
	}

	// Parse days until reset
	if daysCmd.Err() == nil {
		info.DaysUntilReset, _ = strconv.Atoi(daysCmd.Val())
	}

	return info, nil
}

// InitializeUserQuota sets up initial quota for a user.
func InitializeUserQuota(ctx context.Context, client *redis.Client, userID string, planDollars float64, daysUntilReset int) error {
	// Use a transaction to set all fields atomically
	pipe := client.Pipeline()

	pipe.Set(ctx, quotaKey(userID, "plan_percent"), 100, 0)
	pipe.Set(ctx, quotaKey(userID, "plan_dollars"), formatCents(planDollars), 0)
	pipe.Set(ctx, quotaKey(userID, "addon_dollars"), 0, 0)
	pipe.Set(ctx, quotaKey(userID, "reserved"), 0, 0)
	pipe.Set(ctx, quotaKey(userID, "days_until_reset"), daysUntilReset, 0)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize user quota: %w", err)
	}

	return nil
}

// quotaKey generates a Redis key for quota data.
func quotaKey(userID, field string) string {
	return "quota:" + userID + ":" + field
}

// formatCents converts a dollar amount to cents for Redis storage (as integer).
// Redis doesn't support floats in DecrBy, so we multiply by 1000 for 3 decimal places.
func formatCents(dollars float64) int64 {
	return int64(dollars * 1000)
}

// centsToDollars converts integer cents to float64 dollars.
func centsToDollars(cents int64) float64 {
	return float64(cents) / 1000
}
