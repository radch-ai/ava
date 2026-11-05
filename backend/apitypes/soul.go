package apitypes

// SoulConfig is the agent's personality and identity texts (JSONB in
// the DB).
type SoulConfig struct {
	// Personality traits and behavioral characteristics.
	Personality string `json:"personality"`
	// Identity definition and core characteristics.
	Identity string `json:"identity"`
}
