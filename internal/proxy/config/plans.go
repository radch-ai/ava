// Package config provides proxy configuration loading.
package config

// Plan represents a subscription plan with LLM credit allowances
type Plan struct {
	Name              string   `yaml:"name"`
	DisplayName       string   `yaml:"display_name"`
	MonthlyLLMCredits float64  `yaml:"monthly_llm_credits"`
	MaxRequests       int      `yaml:"max_requests,omitempty"`
	Features          []string `yaml:"features,omitempty"`
}

// Plans registry
var plans = map[string]*Plan{
	"basic": {
		Name:              "basic",
		DisplayName:       "Basic",
		MonthlyLLMCredits: 10.00,
		MaxRequests:       1000,
		Features:          []string{"economy_models", "basic_routing"},
	},
	"pro": {
		Name:              "pro",
		DisplayName:       "Pro",
		MonthlyLLMCredits: 50.00,
		MaxRequests:       10000,
		Features:          []string{"all_models", "smart_routing", "priority_queue"},
	},
	"enterprise": {
		Name:              "enterprise",
		DisplayName:       "Enterprise",
		MonthlyLLMCredits: 500.00,
		MaxRequests:       -1, // Unlimited
		Features:          []string{"all_models", "smart_routing", "priority_queue", "custom_models", "dedicated_support"},
	},
}

// GetPlan returns a plan by name
func GetPlan(name string) *Plan {
	return plans[name]
}

// GetAllPlans returns all available plans
func GetAllPlans() []*Plan {
	result := make([]*Plan, 0, len(plans))
	for _, plan := range plans {
		result = append(result, plan)
	}
	return result
}

// GetPlanNames returns all plan names
func GetPlanNames() []string {
	names := make([]string, 0, len(plans))
	for name := range plans {
		names = append(names, name)
	}
	return names
}

// HasFeature checks if a plan has a specific feature
func HasFeature(planName, feature string) bool {
	plan := GetPlan(planName)
	if plan == nil {
		return false
	}

	for _, f := range plan.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// ValidatePlan checks if a plan name is valid
func ValidatePlan(planName string) bool {
	_, exists := plans[planName]
	return exists
}
