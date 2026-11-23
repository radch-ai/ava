package security

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// kindsOf lists the finding kinds in order.
func kindsOf(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

// Known values are replaced by their static marker — never by a
// preview of the value.
func TestScrubKnownValues(t *testing.T) {
	s := NewScrubber([]string{"s3cr3t-value"})
	out, findings := s.ScrubString("Authorization: Bearer s3cr3t-value / trailing")
	if strings.Contains(out, "s3cr3t-value") {
		t.Fatalf("known value survived: %q", out)
	}
	if want := "[REDACTED:secret]"; !strings.Contains(out, want) {
		t.Fatalf("result %q lacks %q", out, want)
	}
	if got := kindsOf(findings); !slices.Equal(got, []string{KnownKind}) {
		t.Fatalf("findings = %v, want [secret]", got)
	}
}

// A secret split by truncation is still matched whole: the scrubber
// runs on the untruncated string, so the marker lands before any
// clipping can break the value.
func TestScrubLongestFirst(t *testing.T) {
	// "abcdef123456" contains "abcdef"; both are known values.
	s := NewScrubber([]string{"abcdef", "abcdef123456"})
	out, _ := s.ScrubString("token=abcdef123456")
	if strings.Contains(out, "abcdef") {
		t.Fatalf("short value matched inside the long one: %q", out)
	}
	if want := "token=" + marker(KnownKind); out != want {
		t.Fatalf("result = %q, want %q", out, want)
	}
}

// Encoded spellings are covered: a URL-encoded value and a Basic-auth
// base64 pair must both redact.
func TestScrubEncodedVariants(t *testing.T) {
	s := NewScrubber([]string{"p@ss:w/rd"})
	for _, probe := range []string{
		"p@ss:w/rd",
		"p%40ss%3Aw%2Frd", // url.QueryEscape
		"cEBzczp3L3Jk",    // base64("p@ss:w/rd")
	} {
		out, findings := s.ScrubString("v=" + probe + ";")
		if strings.Contains(out, probe) {
			t.Errorf("%q survived scrubbing: %q", probe, out)
		}
		if len(findings) == 0 {
			t.Errorf("%q produced no finding", probe)
		}
	}
}

// Every recognizable credential shape is caught, and clean text stays
// byte-identical so sanitization never rewrites safe content.
func TestScrubPatterns(t *testing.T) {
	s := NewPatternScrubber()
	cases := []struct {
		name string
		in   string
		kind string
	}{
		{"openai", "key sk-abcdefghijklmnop1234 here", "api_key"},
		{"openai project", "sk-proj-abcdefghijklmnop1234567890AB", "api_key"},
		{"anthropic", "sk-ant-api03-abcdefghijklmnop1234567890", "api_key"},
		{"stripe live", "sk_" + "live_" + strings.Repeat("0123456789abcdef", 2), "api_key"},
		{"stripe live mixed-case", "sk_" + "live_AbCdEf0123GhIjKlMnOpQr", "api_key"},
		{"stripe restricted", "rk_" + "live_AbCdEf0123GhIjKlMnOpQr", "api_key"},
		{"stripe webhook", "whsec_" + "AbCdEf0123GhIjKlMnOpQrStUv", "webhook_secret"},
		{"ghp", "ghp_0123456789abcdefghijKLMN", "github_token"},
		{"gho", "gho_0123456789abcdefghijKLMN", "github_token"},
		{"ghu", "ghu_" + strings.Repeat("0123456789", 4), "github_token"},
		{"pat", "github_pat_" + "11ABCDEFG0123456789_abcdefghijklmnop", "github_token"},
		{"google", "AIza" + strings.Repeat("SyA-BCdefGHij", 3), "google_key"},
		{"gitlab", "glpat-" + "0123456789abcdefghij", "gitlab_token"},
		{"npm", "npm_" + strings.Repeat("0123456789", 4), "npm_token"},
		{"aws", "AKIA" + "IOSFODNN7EXAMPLE", "aws_key"},
		{"slack", "xoxb-" + "123456789012-abcdefghijklmnop", "slack_token"},
		{"dburl", "postgres://user:hunter2@db.internal:5432/app", "db_url"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "bearer"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----", "private_key"},
	}
	for _, tc := range cases {
		out, findings := s.ScrubString(tc.in)
		if out == tc.in {
			t.Errorf("%s: unchanged, want redaction", tc.name)
			continue
		}
		if strings.Contains(out, "hunter2") || strings.Contains(out, "MIIEow") ||
			strings.Contains(out, "IOSFODNN") || strings.Contains(out, "abcdefghijklmnop") {
			t.Errorf("%s: matched content leaked: %q", tc.name, out)
		}
		if !slices.Contains(kindsOf(findings), tc.kind) {
			t.Errorf("%s: findings = %v, want kind %q", tc.name, kindsOf(findings), tc.kind)
		}
	}

	clean := "no credentials here, just prose about hooks"
	if out, findings := s.ScrubString(clean); out != clean || len(findings) != 0 {
		t.Fatalf("clean text rewritten: %q %v", out, findings)
	}

	// A bare prefix is not a secret: the rule shapes are complete.
	for _, probe := range []string{"sk-", "ghp_", "AKIA", "xoxb-"} {
		if out, _ := s.ScrubString(probe); out != probe {
			t.Errorf("bare prefix %q redacted to %q", probe, out)
		}
	}
}

// Findings are deduplicated by kind: an audit record counts kinds, not
// occurrences, so a hostile body cannot inflate the count column.
func TestScrubFindingsDeduplicated(t *testing.T) {
	s := NewScrubber([]string{"aaaaaaaa"})
	_, findings := s.ScrubString("aaaaaaaa aaaaaaaa aaaaaaaa")
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one per kind", kindsOf(findings))
	}
}

// Structured output is scrubbed per string, never as flat text: a known
// numeric token must not rewrite unrelated numbers.
func TestScrubJSONLeavesNumbersAlone(t *testing.T) {
	s := NewScrubber([]string{"12345678"})

	const numeric = `{"n":1234567890}`
	if out, findings, ok := s.ScrubJSON(numeric); !ok || out != numeric || len(findings) != 0 {
		t.Fatalf("numeric doc = %q %v %v, want unchanged %q", out, kindsOf(findings), ok, numeric)
	}

	out, findings, ok := s.ScrubJSON(`{"tok":"12345678"}`)
	if !ok {
		t.Fatal("ok = false for valid JSON")
	}
	if want := `{"tok":"[REDACTED:secret]"}`; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if got := kindsOf(findings); !slices.Equal(got, []string{KnownKind}) {
		t.Fatalf("findings = %v, want [secret]", got)
	}
}

// Keys and nested strings are scrubbed, and the result stays valid JSON.
func TestScrubJSONKeysAndNesting(t *testing.T) {
	s := NewScrubber([]string{"s3cr3t-value"})
	out, findings, ok := s.ScrubJSON(`{"s3cr3t-value":[{"k":"x","v":"s3cr3t-value"}]}`)
	if !ok {
		t.Fatal("ok = false for valid JSON")
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON: %q", out)
	}
	if strings.Contains(out, "s3cr3t-value") {
		t.Fatalf("secret survived: %q", out)
	}
	if want := `{"[REDACTED:secret]":[{"k":"x","v":"[REDACTED:secret]"}]}`; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if got := kindsOf(findings); !slices.Equal(got, []string{KnownKind}) {
		t.Fatalf("findings = %v, want [secret]", got)
	}
}

// Non-JSON input is reported, not rewritten: the caller falls back to
// flat-text scrubbing.
func TestScrubJSONRejectsNonJSON(t *testing.T) {
	s := NewScrubber([]string{"s3cr3t-value"})
	const in = "s3cr3t-value: not json"
	out, findings, ok := s.ScrubJSON(in)
	if ok {
		t.Fatalf("ok = true for non-JSON input")
	}
	if out != in || findings != nil {
		t.Fatalf("out = %q findings = %v, want unchanged input and no findings", out, kindsOf(findings))
	}
}
