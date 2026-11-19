package names

// Identity-rule unit tests: name validation and id derivation edges the
// endpoint tests don't reach directly (trim, multi-word, case). The
// mock's copies live in ui/web/src/lib/mocks/agent-id.ts.

import "testing"

func TestValid(t *testing.T) {
	valid := []string{"Research", "Research Bot", "A", "Bob 2 Shoes", " Bob "} // trimmed first
	for _, name := range valid {
		if !Valid(name) {
			t.Errorf("Valid(%q) = false, want true", name)
		}
	}
	invalid := []string{"", "A  B", "Bob-1", "_x", "Bob!", "Bot."}
	for _, name := range invalid {
		if Valid(name) {
			t.Errorf("Valid(%q) = true, want false", name)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Research":     "research",
		"Research Bot": "research-bot",
		" Bob ":        "bob", // trimmed before deriving
	}
	for name, want := range cases {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}
