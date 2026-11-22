package security

import (
	"errors"
	"fmt"
)

// Schema complexity bounds. A listing that exceeds any of
// these is rejected as a whole — never truncated, never partially
// accepted.
const (
	MaxSchemaDepth        = 32
	MaxSchemaNodes        = 8192
	MaxSchemaStringLength = 16 << 10 // 16 KiB
)

var (
	// ErrSchemaTooDeep: nesting exceeded MaxSchemaDepth.
	ErrSchemaTooDeep = errors.New("schema too deep")
	// ErrSchemaTooLarge: the value has more than MaxSchemaNodes nodes.
	ErrSchemaTooLarge = errors.New("schema too large")
	// ErrStringTooLong: one string value exceeded MaxSchemaStringLength.
	ErrStringTooLong = errors.New("schema string too long")
)

// ValidateSchema walks a decoded JSON value (as produced by
// json.Unmarshal into any) and enforces the depth, node-count and
// string-length bounds. It resolves nothing: schemas may not trigger
// network or filesystem reads, so $ref is data, not a fetch.
func ValidateSchema(node any) error {
	nodes := 0
	return walkSchema(node, 1, &nodes)
}

// walkSchema visits one value, counting nodes and bounding depth.
func walkSchema(node any, depth int, nodes *int) error {
	if depth > MaxSchemaDepth {
		return fmt.Errorf("%w: depth > %d", ErrSchemaTooDeep, MaxSchemaDepth)
	}
	*nodes++
	if *nodes > MaxSchemaNodes {
		return fmt.Errorf("%w: more than %d nodes", ErrSchemaTooLarge, MaxSchemaNodes)
	}
	switch value := node.(type) {
	case map[string]any:
		for k, v := range value {
			if len(k) > MaxSchemaStringLength {
				return fmt.Errorf("%w: key length %d > %d", ErrStringTooLong, len(k), MaxSchemaStringLength)
			}
			if err := walkSchema(v, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range value {
			if err := walkSchema(v, depth+1, nodes); err != nil {
				return err
			}
		}
	case string:
		if len(value) > MaxSchemaStringLength {
			return fmt.Errorf("%w: length %d > %d", ErrStringTooLong, len(value), MaxSchemaStringLength)
		}
	}
	return nil
}
