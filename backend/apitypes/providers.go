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

// GetProvidersResponse is GET /api/providers' body: every stored
// provider with its custom models.
type GetProvidersResponse struct {
	Providers []Provider `json:"providers"`
}

// ModelEntry is the selection shape everywhere a model is chosen:
// the absolute model id plus the provider row that owns it. nil
// means default routing.
type ModelEntry struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
}

// ModelGroup is one display group in GET /api/models: entries from
// custom ∪ system ids, deduped per provider row with custom winning,
// sorted by id. Providers sharing a display name merge into one
// group; the merge is display-only — entries keep their own
// providerId.
type ModelGroup struct {
	ProviderName string       `json:"providerName"`
	Models       []ModelEntry `json:"models"`
}

// GetModelsResponse is GET /api/models' body: the selector-facing
// catalog. Rows in creation order; providers sharing a display name
// merge into one group.
type GetModelsResponse struct {
	Groups []ModelGroup `json:"groups"`
}

// ProviderModelsResponse is GET /api/providers/{id}/models' body:
// the provider's live-fetched system model ids — custom ids are not
// repeated here.
type ProviderModelsResponse struct {
	SystemModels []string `json:"systemModels"`
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
