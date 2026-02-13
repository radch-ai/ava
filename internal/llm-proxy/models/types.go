package models

// ModelTier represents the quality/cost tier of a model
type ModelTier string

const (
	TierEconomy ModelTier = "economy"
	TierPremium ModelTier = "premium"
	TierUltra   ModelTier = "ultra"
)

// ModelConfig holds pricing and limits for a model
type ModelConfig struct {
	Name                 string    `yaml:"name"`
	Provider             string    `yaml:"provider"`
	InputCostPerMillion  float64   `yaml:"input_cost_per_million"`
	OutputCostPerMillion float64   `yaml:"output_cost_per_million"`
	MaxTokens            int       `yaml:"max_tokens"`
	Tier                 ModelTier `yaml:"tier"`
}
