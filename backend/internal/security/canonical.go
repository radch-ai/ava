package security

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// CanonicalJSON renders v with recursively sorted object keys, so two
// values that differ only in key order produce identical bytes.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("canonical json: decode: %w", err)
	}
	// encoding/json sorts map keys byte-order and emits json.Number
	// literals verbatim, so marshaling the decoded tree is the canonical
	// form. No RFC 8785 claim: only key order is normalized.
	out, err := json.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	return out, nil
}
