package apitypes

// Provider is one catalog connection: display name, base URL and
// whether a key is set. The key itself is write-only - it never
// appears in a response. ids are server-generated uuids, opaque and
// never derived from the name. Models are plain model-id strings.
type Provider struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	BaseURL string   `json:"baseUrl"`
	HasKey  bool     `json:"hasKey"`
	Models  []string `json:"models"`
}

// GetProvidersResponse is GET /api/providers' body: the reserved
// default-routing id plus every provider with its models.
type GetProvidersResponse struct {
	// DefaultModel is the seeded 'auto' row heartbeats and cron jobs
	// fall back to.
	DefaultModel string     `json:"defaultModel"`
	Providers    []Provider `json:"providers"`
}

// CreateProviderResponse is POST /api/providers' 201 body: just the
// generated id.
type CreateProviderResponse struct {
	ID string `json:"id"`
}

// DeleteProviderResponse is DELETE /api/providers/{id}'s 200 body.
type DeleteProviderResponse struct {
	Message string `json:"message"`
}

// CreateProviderRequest is POST /api/providers' body. The key is
// optional - a provider can exist keyless until the user pastes one.
type CreateProviderRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

// UpdateProviderRequest is PATCH /api/providers/{id}'s body: absent
// fields are no-ops (a withheld apiKey keeps the stored key); models
// replaces the list wholesale.
type UpdateProviderRequest struct {
	Name    *string   `json:"name"`
	BaseURL *string   `json:"baseUrl"`
	APIKey  *string   `json:"apiKey"`
	Models  *[]string `json:"models"`
}
