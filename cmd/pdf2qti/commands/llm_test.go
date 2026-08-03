// Whitebox package: selectLLM and stubSummaryJSON are unexported and not otherwise reachable in
// a way that exercises their specific success/edge branches from outside the package (every
// other command test runs with no API key configured, so selectLLM always falls back to the
// stub, and always drives prompts containing at least one chapter heading).
package commands

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
)

type stubLLM struct{}

func (stubLLM) Complete(_ context.Context, _ string) (string, error) { return "stub", nil }

func TestSelectLLM(t *testing.T) {
	tests := []struct {
		name       string
		gen        config.Generation
		setEnv     map[string]string
		wantIsStub bool
	}{
		{
			name:       "unsupported provider falls back to stub",
			gen:        config.Generation{Provider: "anthropic"},
			wantIsStub: true,
		},
		{
			name:       "missing api key falls back to stub",
			gen:        config.Generation{Provider: "openai", APIKeyEnv: "TESTLLM_MISSING_KEY"},
			wantIsStub: true,
		},
		{
			name:       "configured provider returns real client",
			gen:        config.Generation{Provider: "openai", APIKeyEnv: "TESTLLM_KEY"},
			setEnv:     map[string]string{"TESTLLM_KEY": "sk-test"},
			wantIsStub: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not t.Parallel(): t.Setenv forbids parallel ancestry.
			for k, v := range tt.setEnv {
				t.Setenv(k, v)
			}
			logger := audit.New(io.Discard)
			stub := stubLLM{}
			got := selectLLM(tt.gen, logger, stub)
			_, isStub := got.(stubLLM)
			if isStub != tt.wantIsStub {
				t.Fatalf("got isStub=%v, want %v (result type %T)", isStub, tt.wantIsStub, got)
			}
		})
	}
}

func TestStubSummaryJSON_NoChapterHeadingsDefaultsToOneBullet(t *testing.T) {
	t.Parallel()

	got := stubSummaryJSON("prompt with no chapter headings at all")
	if strings.Count(got, `"placeholder"`) != 1 {
		t.Fatalf("expected exactly 1 placeholder bullet when no chapter headings match, got: %s", got)
	}
}
