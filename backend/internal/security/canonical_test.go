package security

import (
	"testing"
)

// Key order is formatting noise: the same value in a different order
// canonicalizes to the same bytes.
func TestCanonicalJSONIgnoresKeyOrder(t *testing.T) {
	a := map[string]any{
		"name":   "echo",
		"schema": map[string]any{"type": "object", "required": []any{"text"}},
	}
	b := map[string]any{
		"schema": map[string]any{"required": []any{"text"}, "type": "object"},
		"name":   "echo",
	}
	ca, err := CanonicalJSON(a)
	if err != nil {
		t.Fatalf("canonical a: %v", err)
	}
	cb, err := CanonicalJSON(b)
	if err != nil {
		t.Fatalf("canonical b: %v", err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("canonical forms differ:\n%s\n%s", ca, cb)
	}
	if string(ca) != `{"name":"echo","schema":{"required":["text"],"type":"object"}}` {
		t.Fatalf("unexpected canonical form: %s", ca)
	}
}
