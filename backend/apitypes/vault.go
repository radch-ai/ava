package apitypes

import "encoding/json"

// PutSecretRequest is the write-only body of PUT /api/vault/{name}.
// The value never appears in any response — a stored secret can only
// be replaced, not re-read over HTTP.
type PutSecretRequest struct {
	Kind  string          `json:"kind"`
	Value string          `json:"value"`
	Meta  json.RawMessage `json:"meta,omitempty"`
}

// SecretMeta is one listed secret: address, type tag and context —
// never the value.
type SecretMeta struct {
	Name string          `json:"name"`
	Kind string          `json:"kind"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

// GetVaultResponse is GET /api/vault's body: metadata for every
// stored secret, ordered by name.
type GetVaultResponse struct {
	Secrets []SecretMeta `json:"secrets"`
}

// PutSecretResponse is PUT /api/vault/{name}'s ack.
type PutSecretResponse struct {
	Message string `json:"message"`
}

// DeleteSecretResponse is DELETE /api/vault/{name}'s ack.
type DeleteSecretResponse struct {
	Message string `json:"message"`
}
