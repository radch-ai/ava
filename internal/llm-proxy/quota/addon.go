package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// AddonPurchase represents an add-on credit purchase.
type AddonPurchase struct {
	UserID       string    `json:"user_id"`
	Amount       float64   `json:"amount"`
	CreditAmount float64   `json:"credit_amount"`
	FeePercent   int       `json:"fee_percent"`
	Timestamp    time.Time `json:"timestamp"`
}

// AddonBalance represents the add-on credit balance for a user.
type AddonBalance struct {
	UserID     string    `json:"user_id"`
	Balance    float64   `json:"balance"`
	TotalAdded float64   `json:"total_added"`
	LastRefill time.Time `json:"last_refill"`
}

// AddCredits adds addon credits to a user's account with fee processing.
// The fee is deducted from the payment amount before credits are added.
func AddCredits(ctx context.Context, client *redis.Client, purchase *AddonPurchase) error {
	// Calculate fee and net credits
	feeAmount := purchase.Amount * float64(purchase.FeePercent) / 100
	netCredits := purchase.Amount - feeAmount

	// Add credits to addon_dollars
	key := quotaKey(purchase.UserID, "addon_dollars")
	err := client.IncrBy(ctx, key, formatCents(netCredits)).Err()
	if err != nil {
		return fmt.Errorf("failed to add credits: %w", err)
	}

	// Update total added counter
	totalKey := quotaKey(purchase.UserID, "addon_total_added")
	client.IncrBy(ctx, totalKey, formatCents(netCredits))

	// Update last refill timestamp
	lastKey := quotaKey(purchase.UserID, "addon_last_refill")
	client.Set(ctx, lastKey, time.Now().Unix(), 0)

	return nil
}

// GetAddonBalance retrieves the add-on credit balance for a user.
func GetAddonBalance(ctx context.Context, client *redis.Client, userID string) (*AddonBalance, error) {
	// Fetch add-on balance fields
	pipe := client.Pipeline()

	balanceCmd := client.Get(ctx, quotaKey(userID, "addon_dollars"))
	totalCmd := client.Get(ctx, quotaKey(userID, "addon_total_added"))
	lastCmd := client.Get(ctx, quotaKey(userID, "addon_last_refill"))

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to fetch addon balance: %w", err)
	}

	balance := &AddonBalance{UserID: userID}

	// Parse balance
	if balanceCmd.Err() == nil {
		cents, _ := balanceCmd.Int64()
		balance.Balance = centsToDollars(cents)
	}

	// Parse total added
	if totalCmd.Err() == nil {
		cents, _ := totalCmd.Int64()
		balance.TotalAdded = centsToDollars(cents)
	}

	// Parse last refill
	if lastCmd.Err() == nil {
		timestamp, _ := lastCmd.Int64()
		balance.LastRefill = time.Unix(timestamp, 0)
	}

	return balance, nil
}

// DeductFromAddon deducts amount from add-on credits.
// Returns error if insufficient balance.
func DeductFromAddon(ctx context.Context, client *redis.Client, userID string, amount float64) error {
	key := quotaKey(userID, "addon_dollars")

	// Get current balance
	cents, err := client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("insufficient addon credits: no balance")
		}
		return fmt.Errorf("failed to get addon balance: %w", err)
	}

	// Check if sufficient balance
	if cents < formatCents(amount) {
		return fmt.Errorf("insufficient addon credits: have %.3f, need %.3f",
			centsToDollars(cents), amount)
	}

	// Deduct amount
	err = client.DecrBy(ctx, key, formatCents(amount)).Err()
	if err != nil {
		return fmt.Errorf("failed to deduct from addon: %w", err)
	}

	return nil
}

// HasAddonBalance checks if user has any add-on credits.
func HasAddonBalance(ctx context.Context, client *redis.Client, userID string) bool {
	key := quotaKey(userID, "addon_dollars")
	cents, err := client.Get(ctx, key).Int64()
	if err != nil {
		return false
	}
	return cents > 0
}

// ClearAddonCredits clears all add-on credits for a user (admin function).
func ClearAddonCredits(ctx context.Context, client *redis.Client, userID string) error {
	key := quotaKey(userID, "addon_dollars")
	return client.Set(ctx, key, 0, 0).Err()
}
