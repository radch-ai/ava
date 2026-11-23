package security

import (
	"encoding/json"
	"fmt"
)

// CanonicalJSON renders v with recursively sorted object keys, so two
// values that differ only in key order produce identical bytes.
// Callers pass decoded trees (UseNumber): encoding/json already sorts
// map keys byte-order at every depth and emits json.Number literals
// verbatim, so one marshal is the canonical form.
func CanonicalJSON(v any) ([]byte, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	return out, nil
}
