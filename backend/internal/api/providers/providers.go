// Package providers serves the provider catalog: stored connections
// with their custom model ids, plus the fetched system lists. Rows
// are user-created; keys are sealed in the runtime vault and never
// echo back (responses carry hasKey); provider ids are server-minted
// uuids.
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
	"github.com/radch-ai/ava/backend/internal/vault"
)

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

// buildProvider renders one provider response from its stored row;
// hasKey is runtime-vault entry existence.
func buildProvider(ctx context.Context, queries *db.Queries, vaultStore *vault.Store, id string) (apitypes.Provider, error) {
	row, err := queries.GetProvider(ctx, id)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read provider: %w", err)
	}
	custom, err := decodeModels(row.Models)
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("decode models for provider %q: %w", id, err)
	}
	hasKey, err := vaultStore.Has(ctx, vault.NameRuntime, ProviderEntryName(id))
	if err != nil {
		return apitypes.Provider{}, fmt.Errorf("read key state for provider %q: %w", id, err)
	}
	return apitypes.Provider{
		ID:      row.ID,
		Name:    row.Name,
		BaseURL: row.BaseUrl,
		HasKey:  hasKey,
		Models:  custom,
	}, nil
}

// ProviderEntryName is the runtime-vault name of a provider's key
// entry; kindProvider is its kind.
func ProviderEntryName(id string) string { return "provider:" + id }

const kindProvider = "provider"

// providerKey reads the provider's stored key; "" when none is stored
// or the entry does not open — fail-closed at this read.
func providerKey(ctx context.Context, vaultStore *vault.Store, id string) string {
	plaintext, _, err := vaultStore.Get(ctx, vault.NameRuntime, ProviderEntryName(id))
	if err != nil {
		return ""
	}
	return string(plaintext)
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
