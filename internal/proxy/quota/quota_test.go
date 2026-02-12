package quota

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// mockRedis creates a mock redis server for testing
func mockRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	return s, client
}

func TestCheckQuota(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: Initialize user with plan quota (stored as integer thousandths)
	// 10.00 dollars = 10000 cents
	key := "quota:user-123:plan_dollars"
	client.Set(ctx, key, 10000, 0)

	// Test: Check quota for a small cost
	result, err := CheckQuota(ctx, client, "user-123", 0.002)
	if err != nil {
		t.Fatalf("CheckQuota failed: %v", err)
	}

	if !result.HasQuota {
		t.Error("Expected quota to be available")
	}

	if result.Source != "plan" {
		t.Errorf("Expected source 'plan', got '%s'", result.Source)
	}
}

func TestCheckQuotaInsufficient(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: User with very low balance (0.001 dollars = 1 cent)
	key := "quota:user-456:plan_dollars"
	client.Set(ctx, key, 1, 0)

	// Test: Check quota for a larger cost (0.002 dollars = 2 cents)
	result, err := CheckQuota(ctx, client, "user-456", 0.002)
	if err != nil {
		t.Fatalf("CheckQuota failed: %v", err)
	}

	if result.HasQuota {
		t.Error("Expected quota to be insufficient")
	}
}

func TestCheckQuotaWithAddon(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: User with depleted plan but available addon
	client.Set(ctx, "quota:user-789:plan_dollars", 1, 0)     // 0.001 dollars
	client.Set(ctx, "quota:user-789:addon_dollars", 5000, 0) // 5.00 dollars

	// Test: Check quota should fall back to addon
	result, err := CheckQuota(ctx, client, "user-789", 0.002)
	if err != nil {
		t.Fatalf("CheckQuota failed: %v", err)
	}

	if !result.HasQuota {
		t.Error("Expected quota to be available from addon")
	}

	if result.Source != "addon" {
		t.Errorf("Expected source 'addon', got '%s'", result.Source)
	}

	if !result.UsingAddon {
		t.Error("Expected UsingAddon to be true")
	}
}

func TestReserveQuota(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: Initialize user with plan quota (10.00 dollars = 10000 cents)
	client.Set(ctx, "quota:user-reserve:plan_dollars", 10000, 0)

	// Test: Reserve quota (0.002 dollars = 2 cents)
	err := ReserveQuota(ctx, client, "user-reserve", 0.002, "plan")
	if err != nil {
		t.Fatalf("ReserveQuota failed: %v", err)
	}

	// Verify: Check that quota was reserved (10000 - 2 = 9998 = 9.998 dollars)
	remaining, err := client.Get(ctx, "quota:user-reserve:plan_dollars").Result()
	if err != nil {
		t.Fatalf("Failed to get remaining quota: %v", err)
	}

	expected := "9998"
	if remaining != expected {
		t.Errorf("Expected remaining %s, got %s", expected, remaining)
	}

	// Verify reserved amount (2 cents)
	reserved, err := client.Get(ctx, "quota:user-reserve:reserved").Result()
	if err != nil {
		t.Fatalf("Failed to get reserved quota: %v", err)
	}

	if reserved != "2" {
		t.Errorf("Expected reserved 2, got %s", reserved)
	}
}

func TestDeductQuotaOverReserved(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: User with reserved quota
	client.Set(ctx, "quota:user-deduct1:plan_dollars", 10000, 0) // 10.00 dollars
	client.Set(ctx, "quota:user-deduct1:reserved", 10, 0)        // 0.010 dollars

	// Test: Actual cost is less than reserved (refund 0.005)
	err := DeductQuota(ctx, client, "user-deduct1", 0.010, 0.005, "plan")
	if err != nil {
		t.Fatalf("DeductQuota failed: %v", err)
	}

	// Verify: 5 cents should be refunded (10000 + 5 = 10005 = 10.005 dollars)
	remaining, _ := client.Get(ctx, "quota:user-deduct1:plan_dollars").Result()
	if remaining != "10005" {
		t.Errorf("Expected refund, got %s", remaining)
	}

	// Verify reservation is cleared
	reserved, _ := client.Get(ctx, "quota:user-deduct1:reserved").Result()
	if reserved != "0" {
		t.Errorf("Expected reservation cleared, got %s", reserved)
	}
}

func TestDeductQuotaUnderReserved(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: User with reserved quota
	client.Set(ctx, "quota:user-deduct2:plan_dollars", 10000, 0) // 10.00 dollars
	client.Set(ctx, "quota:user-deduct2:reserved", 5, 0)         // 0.005 dollars

	// Test: Actual cost is more than reserved (charge additional 0.005)
	err := DeductQuota(ctx, client, "user-deduct2", 0.005, 0.010, "plan")
	if err != nil {
		t.Fatalf("DeductQuota failed: %v", err)
	}

	// Verify: Additional 5 cents should be charged (10000 - 5 = 9995 = 9.995 dollars)
	remaining, _ := client.Get(ctx, "quota:user-deduct2:plan_dollars").Result()
	if remaining != "9995" {
		t.Errorf("Expected additional charge, got %s", remaining)
	}

	// Verify reservation is cleared
	reserved, _ := client.Get(ctx, "quota:user-deduct2:reserved").Result()
	if reserved != "0" {
		t.Errorf("Expected reservation cleared, got %s", reserved)
	}
}

func TestGetUserQuotaInfo(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: User quota data (stored as integer cents)
	client.Set(ctx, "quota:user-info:plan_percent", 75, 0)
	client.Set(ctx, "quota:user-info:plan_dollars", 7500, 0)  // 7.50 dollars
	client.Set(ctx, "quota:user-info:addon_dollars", 2000, 0) // 2.00 dollars
	client.Set(ctx, "quota:user-info:reserved", 500, 0)       // 0.50 dollars
	client.Set(ctx, "quota:user-info:days_until_reset", 15, 0)

	// Test: Get user quota info
	info, err := GetUserQuotaInfo(ctx, client, "user-info")
	if err != nil {
		t.Fatalf("GetUserQuotaInfo failed: %v", err)
	}

	if info.PlanPercent != 75 {
		t.Errorf("Expected PlanPercent 75, got %d", info.PlanPercent)
	}

	if info.PlanDollars != 7.50 {
		t.Errorf("Expected PlanDollars 7.50, got %f", info.PlanDollars)
	}

	if info.AddonDollars != 2.00 {
		t.Errorf("Expected AddonDollars 2.00, got %f", info.AddonDollars)
	}

	if info.Reserved != 0.50 {
		t.Errorf("Expected Reserved 0.50, got %f", info.Reserved)
	}

	if info.DaysUntilReset != 15 {
		t.Errorf("Expected DaysUntilReset 15, got %d", info.DaysUntilReset)
	}
}

func TestInitializeUserQuota(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Test: Initialize user quota
	err := InitializeUserQuota(ctx, client, "user-new", 100.00, 30)
	if err != nil {
		t.Fatalf("InitializeUserQuota failed: %v", err)
	}

	// Verify plan_percent
	percent, _ := client.Get(ctx, "quota:user-new:plan_percent").Result()
	if percent != "100" {
		t.Errorf("Expected plan_percent 100, got %s", percent)
	}

	// Verify plan_dollars (100.00 dollars = 100000 cents)
	dollars, _ := client.Get(ctx, "quota:user-new:plan_dollars").Result()
	if dollars != "100000" {
		t.Errorf("Expected plan_dollars 100000, got %s", dollars)
	}

	// Verify addon_dollars (0)
	addon, _ := client.Get(ctx, "quota:user-new:addon_dollars").Result()
	if addon != "0" {
		t.Errorf("Expected addon_dollars 0, got %s", addon)
	}

	// Verify days_until_reset
	days, _ := client.Get(ctx, "quota:user-new:days_until_reset").Result()
	if days != "30" {
		t.Errorf("Expected days_until_reset 30, got %s", days)
	}
}
