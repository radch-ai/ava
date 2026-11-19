// Package names holds the repo's display-name convention, shared by
// every entity whose id derives from a user-facing name (agents, MCP
// servers): ASCII letters and digits with single spaces between words,
// slugged to a lowercase-hyphen id. One rule, one place — the create
// endpoints must accept and reject the same shapes.
package names

import (
	"regexp"
	"strings"
)

// validRE: ASCII letters, digits and single spaces between words,
// tested on the trimmed name.
var validRE = regexp.MustCompile(`^[A-Za-z0-9]+(?: [A-Za-z0-9]+)*$`)

// Valid reports whether name matches the convention.
func Valid(name string) bool {
	return validRE.MatchString(strings.TrimSpace(name))
}

// Slug maps a validated name to its id: lowercase, spaces as hyphens.
// Only the shapes Valid admits reach here, so no further normalization
// applies — and ids can never start with "_", the system-reserved
// prefix.
func Slug(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}
