package quota

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Service provides quota management operations with Redis backend.
type Service struct {
	client *redis.Client
}

// NewService creates a new quota service with Redis backend.
func NewService(client *redis.Client) *Service {
	return &Service{
		client: client,
	}
}

// Check performs a quota check for a user.
func (s *Service) Check(ctx context.Context, userID string, estimatedCost float64) (*CheckResult, error) {
	return CheckQuota(ctx, s.client, userID, estimatedCost)
}

// Reserve reserves quota before an LLM call.
func (s *Service) Reserve(ctx context.Context, userID string, estimatedCost float64, source string) error {
	return ReserveQuota(ctx, s.client, userID, estimatedCost, source)
}

// Deduct adjusts quota after a streaming response completes.
func (s *Service) Deduct(ctx context.Context, userID string, reserved, actual float64, source string) error {
	return DeductQuota(ctx, s.client, userID, reserved, actual, source)
}

// GetInfo retrieves quota information for a user.
func (s *Service) GetInfo(ctx context.Context, userID string) (*UserQuotaInfo, error) {
	return GetUserQuotaInfo(ctx, s.client, userID)
}

// Initialize sets up initial quota for a new user.
func (s *Service) Initialize(ctx context.Context, userID string, planDollars float64, daysUntilReset int) error {
	return InitializeUserQuota(ctx, s.client, userID, planDollars, daysUntilReset)
}

// AddCredits adds addon credits to a user's account.
func (s *Service) AddCredits(ctx context.Context, userID string, amount float64) error {
	// Increment addon_dollars
	key := quotaKey(userID, "addon_dollars")
	err := s.client.IncrBy(ctx, key, formatCents(amount)).Err()
	if err != nil {
		return fmt.Errorf("failed to add credits: %w", err)
	}
	return nil
}

// SetPlanQuota updates a user's plan quota.
func (s *Service) SetPlanQuota(ctx context.Context, userID string, planDollars float64, planPercent int, daysUntilReset int) error {
	pipe := s.client.Pipeline()

	pipe.Set(ctx, quotaKey(userID, "plan_dollars"), formatCents(planDollars), 0)
	pipe.Set(ctx, quotaKey(userID, "plan_percent"), planPercent, 0)
	pipe.Set(ctx, quotaKey(userID, "days_until_reset"), daysUntilReset, 0)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to set plan quota: %w", err)
	}

	return nil
}

// ResetDaily resets daily usage counters for a user.
func (s *Service) ResetDaily(ctx context.Context, userID string) error {
	// Reset daily-used counter if it exists
	key := "usage:" + userID + ":daily_tokens"
	return s.client.Del(ctx, key).Err()
}

// GetReservedAmount returns the currently reserved amount for a user.
func (s *Service) GetReservedAmount(ctx context.Context, userID string) (float64, error) {
	key := quotaKey(userID, "reserved")
	cents, err := s.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get reserved amount: %w", err)
	}
	return centsToDollars(cents), nil
}

// ReleaseAllReservations releases all reserved quota for a user (emergency function).
func (s *Service) ReleaseAllReservations(ctx context.Context, userID string) error {
	// Get current reserved amount
	key := quotaKey(userID, "reserved")
	cents, err := s.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return nil // No reservations to release
		}
		return fmt.Errorf("failed to get reserved amount: %w", err)
	}

	if cents == 0 {
		return nil // Nothing to release
	}

	// Get plan_dollars
	planKey := quotaKey(userID, "plan_dollars")
	planCents, err := s.client.Get(ctx, planKey).Int64()
	if err != nil {
		return fmt.Errorf("failed to get plan dollars: %w", err)
	}

	// Refund reserved amount to plan
	err = s.client.Set(ctx, planKey, planCents+cents, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to refund reservations: %w", err)
	}

	// Clear reservations
	err = s.client.Set(ctx, key, 0, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to clear reservations: %w", err)
	}

	return nil
}
