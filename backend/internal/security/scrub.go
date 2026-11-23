// Package security holds the host's boundary primitives: secret
// scrubbing, canonical JSON, schema complexity limits, and destination
// classification. Pure
// functions and small structs — every caller (mcp, mcpserver,
// tooladmission, gateway) uses the same implementation, so a policy
// cannot drift between paths.
package security

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Redaction markers are static: a kind, never a prefix, suffix or any
// other shadow of the matched content.
const KnownKind = "secret"

// Finding is one redaction that happened: the classified kind only,
// never the matched value.
type Finding struct {
	Kind string
}

// Scrubber replaces known secret values and high-confidence credential
// patterns with static markers.
type Scrubber struct {
	// known holds the registered spellings of every known value,
	// longest first, so a longer secret is replaced before a shorter
	// one it contains can split it.
	known []string
	// numericKnown holds the canonical form of every known value that
	// is a JSON number, so alternate spellings of the same value
	// (12345, 12345.0, 1.2345e4) match in the findings walk. It is a
	// comparison set only — text scrubbing stays exact-spelling.
	numericKnown map[string]struct{}
	// patterns run after known values: a recognized shape is a leak
	// even when its value was never registered.
	patterns []pattern
}

type pattern struct {
	kind string
	re   *regexp.Regexp
}

// patternBattery is the validated set: complete rule shapes, no
// prefix-only heuristics, no fuzzy matching. Compiled once; regexes are
// safe for concurrent use, so every scrubber shares the slice.
var patternBattery = func() []pattern {
	must := func(kind, expr string) pattern {
		return pattern{kind: kind, re: regexp.MustCompile(expr)}
	}
	return []pattern{
		must("private_key", `(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
		must("private_key", `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
		must("db_url", `[A-Za-z][A-Za-z0-9+.\-]*://[^/\s:@]+:[^/\s@]+@[^\s/]+`),
		must("github_token", `github_pat_[A-Za-z0-9_]{20,}`),
		must("github_token", `ghp_[A-Za-z0-9]{20,}`),
		must("github_token", `gho_[A-Za-z0-9]{20,}`),
		must("github_token", `gh[pousr]_[0-9A-Za-z]{36,}`),
		must("aws_key", `AKIA[0-9A-Z]{16}`),
		must("google_key", `AIza[0-9A-Za-z_-]{30,}`),
		must("gitlab_token", `glpat-[0-9A-Za-z-]{20,}`),
		must("npm_token", `npm_[0-9A-Za-z]{30,}`),
		must("slack_token", `xox[baprs]-[A-Za-z0-9-]{10,}`),
		must("bearer", `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}`),
		must("api_key", `sk-[A-Za-z0-9]{16,}`),
		must("api_key", `sk-(proj|ant)-[A-Za-z0-9_-]{16,}`),
		must("api_key", `sk_live_[0-9A-Za-z]{16,}`),
		must("api_key", `rk_live_[0-9A-Za-z]{16,}`),
		must("webhook_secret", `whsec_[0-9A-Za-z]{24,}`),
	}
}()

// NewPatternScrubber scrubs recognizable credential shapes only —
// the path for text whose exact secret values are not known (listings,
// diagnostics, tool metadata).
func NewPatternScrubber() *Scrubber {
	return &Scrubber{patterns: patternBattery}
}

// NewScrubber scrubs exact known values plus the pattern battery. Each
// value also registers its URL-encoded spelling and, for `user:pass`
// shaped values, its Basic-auth base64 form — the representations that
// actually reach the wire.
func NewScrubber(knownValues []string) *Scrubber {
	seen := make(map[string]struct{}, len(knownValues)*3)
	var known []string
	add := func(v string) {
		if v == "" {
			return
		}
		if _, dup := seen[v]; dup {
			return
		}
		seen[v] = struct{}{}
		known = append(known, v)
	}
	for _, v := range knownValues {
		add(v)
		add(url.QueryEscape(v))
		if user, pass, ok := strings.Cut(v, ":"); ok && user != "" && pass != "" {
			add(base64.StdEncoding.EncodeToString([]byte(user + ":" + pass)))
		}
	}
	slices.SortStableFunc(known, func(a, b string) int { return len(b) - len(a) })
	numeric := make(map[string]struct{})
	for _, v := range knownValues {
		if c := canonicalNumber(v); c != "" {
			numeric[c] = struct{}{}
		}
	}
	return &Scrubber{known: known, numericKnown: numeric, patterns: patternBattery}
}

// canonicalNumber returns one spelling for every JSON number literal
// that denotes the same value — 12345, 12345.0 and 1.2345e4 all
// compare equal. Anything that is not a number returns "".
func canonicalNumber(literal string) string {
	r, ok := new(big.Rat).SetString(literal)
	if !ok {
		return ""
	}
	if r.IsInt() {
		return r.Num().String()
	}
	return r.RatString()
}

// ScrubString returns the sanitized copy and the findings that
// produced it. Redaction is one-way: the caller only ever sees the
// marker form.
func (s *Scrubber) ScrubString(in string) (string, []Finding) {
	if in == "" || s == nil {
		return in, nil
	}
	out := in
	var findings []Finding
	note := func(kind string) {
		if !slices.ContainsFunc(findings, func(f Finding) bool { return f.Kind == kind }) {
			findings = append(findings, Finding{Kind: kind})
		}
	}
	for _, v := range s.known {
		if !strings.Contains(out, v) {
			continue
		}
		out = strings.ReplaceAll(out, v, marker(KnownKind))
		note(KnownKind)
	}
	for _, p := range s.patterns {
		if !p.re.MatchString(out) {
			continue
		}
		out = p.re.ReplaceAllString(out, marker(p.kind))
		note(p.kind)
	}
	return out, findings
}

// marker renders one redaction placeholder for a kind.
func marker(kind string) string {
	return "[REDACTED:" + kind + "]"
}

// ScrubJSON redacts known values and pattern matches inside string values
// and keys of a JSON document, leaving numbers untouched, and re-encodes
// compactly with sorted keys. ok=false when in is not valid JSON.
//
// Structured output must never be scrubbed as flat text: a numeric token
// that happens to equal a known value would corrupt unrelated numbers,
// and replacing inside JSON syntax could produce invalid JSON.
func (s *Scrubber) ScrubJSON(in string) (out string, findings []Finding, ok bool) {
	if s == nil {
		return in, nil, false
	}
	if !json.Valid([]byte(in)) {
		return in, nil, false
	}
	// Fast path: when the document carries no escape sequences, the
	// raw-text scan sees every string in full — a clean scan means
	// nothing to rewrite, and the decode-and-rewrite pass is skipped.
	// Escaped spellings (c\u0061nary) only decode inside the structured
	// walk, so any backslash takes the full path.
	if !strings.ContainsRune(in, '\\') {
		if _, fast := s.ScrubString(in); len(fast) == 0 {
			return in, nil, true
		}
	}
	dec := json.NewDecoder(strings.NewReader(in))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return in, nil, false
	}
	scrubbed, findings := s.scrubValue(tree)
	encoded, err := json.Marshal(scrubbed)
	if err != nil {
		return in, nil, false
	}
	return string(encoded), findings, true
}

// scrubValue walks one decoded JSON value, scrubbing every string —
// object keys included — and leaving numbers and booleans untouched.
func (s *Scrubber) scrubValue(v any) (any, []Finding) {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		var findings []Finding
		for k, item := range value {
			key, keyFindings := s.ScrubString(k)
			scrubbed, itemFindings := s.scrubValue(item)
			out[key] = scrubbed
			findings = noteFindings(findings, keyFindings)
			findings = noteFindings(findings, itemFindings)
		}
		return out, findings
	case []any:
		out := make([]any, len(value))
		var findings []Finding
		for i, item := range value {
			scrubbed, itemFindings := s.scrubValue(item)
			out[i] = scrubbed
			findings = noteFindings(findings, itemFindings)
		}
		return out, findings
	case string:
		return s.ScrubString(value)
	default:
		return value, nil
	}
}

// ScrubFindingsIn reports the redaction kinds a decoded JSON value needs,
// without rewriting it: every string and object key is inspected, and
// numeric leaves are compared against the known values' spellings —
// exact first, then by canonical value, so an alternate literal
// (1.2345e4 for 12345) is still that credential.
// The path for authority-bearing input that must be rejected rather
// than sanitized.
func (s *Scrubber) ScrubFindingsIn(value any) []Finding {
	if s == nil {
		return nil
	}
	var findings []Finding
	var walk func(v any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for k, item := range value {
				_, keyFindings := s.ScrubString(k)
				findings = noteFindings(findings, keyFindings)
				walk(item)
			}
		case []any:
			for _, item := range value {
				walk(item)
			}
		case string:
			_, itemFindings := s.ScrubString(value)
			findings = noteFindings(findings, itemFindings)
		case json.Number:
			for _, known := range s.known {
				if value.String() == known {
					findings = noteFindings(findings, []Finding{{Kind: KnownKind}})
					break
				}
			}
			if c := canonicalNumber(value.String()); c != "" {
				if _, ok := s.numericKnown[c]; ok {
					findings = noteFindings(findings, []Finding{{Kind: KnownKind}})
				}
			}
		}
	}
	walk(value)
	return findings
}

// noteFindings appends the kinds not already recorded, so a document
// reports each redaction kind once.
func noteFindings(into []Finding, add []Finding) []Finding {
	for _, f := range add {
		if !slices.ContainsFunc(into, func(existing Finding) bool { return existing.Kind == f.Kind }) {
			into = append(into, f)
		}
	}
	return into
}

// CredentialForms expands attachment values into every spelling that can
// reach a wire or a log: the value itself, a scheme-prefixed token's bare
// token ("Bearer <token>", "Basic <b64>", "Token <x>"), and their
// URL-encoded forms. Drives scrubber registration for a server's headers.
func CredentialForms(values []string) []string {
	forms := make([]string, 0, len(values)*2)
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		forms = append(forms, v)
		if fields := strings.Fields(v); len(fields) == 2 {
			forms = append(forms, fields[1])
		}
	}
	return forms
}
