// Package providers serves the provider catalog: stored connections
// with their custom model ids, plus the fetched system lists. Rows
// are user-created; api_key is write-only (responses carry hasKey);
// provider ids are server-minted uuids.
package providers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
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

// buildProvider renders one provider response from its stored row.
func buildProvider(ctx context.Context, queries *db.Queries, id string) (apitypes.Provider, error) {
	row, err := queries.GetProvider(ctx, id)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read provider: %w", err)
	}
	custom, err := decodeModels(row.Models)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("decode models for provider %q: %w", id, err)
	}
	return apitypes.Provider{
		ID:      row.ID,
		Name:    row.Name,
		BaseURL: row.BaseUrl,
		HasKey:  row.ApiKey != "",
		Models:  custom,
	}, nil
}

// decodeModels parses the providers.models JSON array column.
func decodeModels(s string) ([]string, error) {
	if s == "" {
		return []string{}, nil
	}
	var models []string
	if err := json.Unmarshal([]byte(s), &models); err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}
	return models, nil
}

// encodeModels writes the custom list back to the column shape,
// canonically sorted (a string slice marshal cannot fail).
func encodeModels(models []string) string {
	sorted := append([]string(nil), models...)
	slices.Sort(sorted)
	b, _ := json.Marshal(sorted)
	return string(b)
}
