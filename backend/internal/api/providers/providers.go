// Package providers serves /api/providers: the model catalog, one
// row per connection with its model-id strings. api_key is
// write-only (responses carry hasKey); provider ids are server
// minted uuids, model ids plain unique strings.
package providers

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// DefaultModel is the routing fallback heartbeats and cron jobs
// land on when their model is unset or was dropped.
const DefaultModel = "auto"

// newUUID returns a random v4 uuid string.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// isValidBaseURL: an absolute http(s) URL.
func isValidBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// buildProvider renders one provider with its models.
func buildProvider(ctx context.Context, queries *db.Queries, id string) (apitypes.Provider, error) {
	row, err := queries.GetProvider(ctx, id)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read provider: %w", err)
	}
	models, err := queries.ListProviderModels(ctx, id)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("list provider models: %w", err)
	}
	return apitypes.Provider{
		ID:      row.ID,
		Name:    row.Name,
		BaseURL: row.BaseUrl,
		HasKey:  row.ApiKey != "",
		Models:  models,
	}, nil
}
