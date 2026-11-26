package agent

// The /compact fit check needs a token count, and the gateway talks to
// many providers, so no single native tokenizer is authoritative here.
// A local BPE count is the honest approximation: same class as every
// harness that fronts third-party models, and correct on CJK and
// punctuation where a chars/4 rule is not.

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

var (
	encOnce sync.Once
	enc     *tiktoken.Tiktoken
	encErr  error
)

// estimateTokens counts text with the o200k_base BPE vocabulary, which
// the loader embeds — no runtime fetch. The counts approximate every
// provider's own tokenizer; the window comparison only needs the same
// order of magnitude. On the (unreachable) case where the encoding
// cannot be built, a chars/4 floor keeps /compact working rather than
// failing every summarization.
func estimateTokens(text string) int {
	encOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
		enc, encErr = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	})
	if encErr != nil {
		return (len(text) + 3) / 4
	}
	return len(enc.Encode(text, nil, nil))
}
