package backend_test

// The rotation prompt over piped input; the hidden-echo terminal path
// is the same loop with term.ReadPassword underneath.
import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend"
)

func TestPromptNewMasterKeyAccepts(t *testing.T) {
	key := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	var out bytes.Buffer
	got, err := backend.PromptNewMasterKey(strings.NewReader(key+"\n"+key+"\n"), &out)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := hex.EncodeToString(got); got != key {
		t.Fatalf("key = %s, want %s", got, key)
	}
}

// A mismatch retries the whole pair; a bad hex line reports and
// retries.
func TestPromptNewMasterKeyRetries(t *testing.T) {
	key := hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	cases := []struct {
		name  string
		input string
	}{
		{"mismatch then match", "00" + key[2:] + "\n" + key + "\n" + key + "\n" + key + "\n"},
		{"bad hex then valid", "not hex\n" + key + "\n" + key + "\n"},
		{"empty line then valid", "\n" + key + "\n" + key + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if _, err := backend.PromptNewMasterKey(strings.NewReader(tc.input), &out); err != nil {
				t.Fatalf("prompt: %v (out %s)", err, out.String())
			}
		})
	}
}

func TestPromptNewMasterKeyAborts(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"eof at first entry", ""},
		{"eof at confirm", strings.Repeat("0", 64) + "\n"},
		{"three bad attempts", "a\nb\nc\nd\ne\nf\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if _, err := backend.PromptNewMasterKey(strings.NewReader(tc.input), &out); err == nil {
				t.Fatal("prompt = nil error, want abort")
			}
		})
	}
}
