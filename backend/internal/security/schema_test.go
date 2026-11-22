package security

import (
	"errors"
	"strings"
	"testing"
)

// nestedSchema builds the depth boundary shape: a chain of nested
// arrays with the leaf at exactly d levels below the root.
func nestedSchema(depth int) any {
	var node any = "leaf"
	for i := 1; i < depth; i++ {
		node = []any{node}
	}
	return node
}

// The depth bound admits exactly MaxSchemaDepth and rejects one more.
func TestValidateSchemaDepthBoundary(t *testing.T) {
	if err := ValidateSchema(nestedSchema(MaxSchemaDepth)); err != nil {
		t.Fatalf("depth %d rejected: %v", MaxSchemaDepth, err)
	}
	err := ValidateSchema(nestedSchema(MaxSchemaDepth + 1))
	if !errors.Is(err, ErrSchemaTooDeep) {
		t.Fatalf("depth %d err = %v, want ErrSchemaTooDeep", MaxSchemaDepth+1, err)
	}
}

// The node bound admits exactly MaxSchemaNodes and rejects one more.
func TestValidateSchemaNodeBoundary(t *testing.T) {
	// A flat array is one node per element plus the root and itself.
	exact := []any{make([]any, MaxSchemaNodes-2)}
	if err := ValidateSchema(exact); err != nil {
		t.Fatalf("%d nodes rejected: %v", MaxSchemaNodes, err)
	}
	over := []any{make([]any, MaxSchemaNodes-1)}
	if err := ValidateSchema(over); !errors.Is(err, ErrSchemaTooLarge) {
		t.Fatalf("%d nodes err = %v, want ErrSchemaTooLarge", MaxSchemaNodes+1, err)
	}
}

// The string bound admits exactly 16 KiB and rejects one byte more,
// for values and for keys.
func TestValidateSchemaStringBoundary(t *testing.T) {
	atLimit := strings.Repeat("a", MaxSchemaStringLength)
	if err := ValidateSchema(map[string]any{"description": atLimit}); err != nil {
		t.Fatalf("string at limit rejected: %v", err)
	}
	overLimit := strings.Repeat("a", MaxSchemaStringLength+1)
	if err := ValidateSchema(overLimit); !errors.Is(err, ErrStringTooLong) {
		t.Fatalf("string over limit err = %v, want ErrStringTooLong", err)
	}
	if err := ValidateSchema(map[string]any{overLimit: "x"}); !errors.Is(err, ErrStringTooLong) {
		t.Fatalf("key over limit err = %v, want ErrStringTooLong", err)
	}
}

// A plain scalar schema and non-schema scalars pass: the validator
// bounds work, it does not judge shape.
func TestValidateSchemaAcceptsScalars(t *testing.T) {
	for _, v := range []any{nil, true, float64(1), "text", []any{}, map[string]any{}} {
		if err := ValidateSchema(v); err != nil {
			t.Fatalf("%v rejected: %v", v, err)
		}
	}
}
