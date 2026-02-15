// Package config provides configuration loading for the gateway.
package config

import (
	"testing"
)

func TestGetPlan(t *testing.T) {
	plan := GetPlan("basic")
	if plan == nil {
		t.Fatal("GetPlan returned nil for basic")
	}

	if plan.Name != "basic" {
		t.Errorf("Expected plan name 'basic', got %s", plan.Name)
	}
}

func TestGetAllPlans(t *testing.T) {
	plans := GetAllPlans()
	if len(plans) == 0 {
		t.Error("GetAllPlans returned empty list")
	}

	// Check that expected plans exist
	expectedPlans := []string{"basic", "pro", "enterprise"}
	for _, name := range expectedPlans {
		found := false
		for _, plan := range plans {
			if plan.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Plan %s not found", name)
		}
	}
}

func TestPlanCredits(t *testing.T) {
	plan := GetPlan("pro")
	if plan == nil {
		t.Fatal("GetPlan returned nil for pro")
	}

	if plan.MonthlyLLMCredits <= 0 {
		t.Errorf("Expected positive credits for pro plan, got %f", plan.MonthlyLLMCredits)
	}
}

func TestGetPlanNames(t *testing.T) {
	names := GetPlanNames()
	if len(names) == 0 {
		t.Error("GetPlanNames returned empty list")
	}

	expectedPlans := []string{"basic", "pro", "enterprise"}
	for _, expected := range expectedPlans {
		found := false
		for _, name := range names {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Plan name %s not found", expected)
		}
	}
}

func TestHasFeature(t *testing.T) {
	tests := []struct {
		name     string
		plan     string
		feature  string
		expected bool
	}{
		{
			name:     "basic has economy_models",
			plan:     "basic",
			feature:  "economy_models",
			expected: true,
		},
		{
			name:     "basic does not have all_models",
			plan:     "basic",
			feature:  "all_models",
			expected: false,
		},
		{
			name:     "pro has all_models",
			plan:     "pro",
			feature:  "all_models",
			expected: true,
		},
		{
			name:     "enterprise has custom_models",
			plan:     "enterprise",
			feature:  "custom_models",
			expected: true,
		},
		{
			name:     "invalid plan returns false",
			plan:     "invalid",
			feature:  "any_feature",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasFeature(tt.plan, tt.feature)
			if result != tt.expected {
				t.Errorf("HasFeature(%q, %q) = %v, want %v", tt.plan, tt.feature, result, tt.expected)
			}
		})
	}
}

func TestValidatePlan(t *testing.T) {
	tests := []struct {
		name     string
		plan     string
		expected bool
	}{
		{
			name:     "valid basic plan",
			plan:     "basic",
			expected: true,
		},
		{
			name:     "valid pro plan",
			plan:     "pro",
			expected: true,
		},
		{
			name:     "valid enterprise plan",
			plan:     "enterprise",
			expected: true,
		},
		{
			name:     "invalid plan",
			plan:     "invalid",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePlan(tt.plan)
			if result != tt.expected {
				t.Errorf("ValidatePlan(%q) = %v, want %v", tt.plan, result, tt.expected)
			}
		})
	}
}
