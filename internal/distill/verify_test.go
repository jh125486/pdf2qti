// Whitebox package: verifyConsistency and buildVerifyPrompt are unexported.
package distill

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type staticLLM struct {
	response string
	err      error
}

func (s *staticLLM) Complete(_ context.Context, _ string) (string, error) {
	return s.response, s.err
}

func TestVerifyConsistency_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		llm          *staticLLM
		wantErr      bool
		wantWarnings []string
	}{
		{
			name: "dedups and reports warnings",
			llm: &staticLLM{response: `{
				"vocabulary": [{"term":"Vector","definition":"magnitude and direction"}],
				"sections": [{"title":"Vectors","summary":"merged summary"}],
				"warnings": ["magnitude formula stated two ways"]
			}`},
			wantWarnings: []string{"magnitude formula stated two ways"},
		},
		{name: "no warnings", llm: &staticLLM{response: `{"vocabulary":[],"sections":[],"warnings":[]}`}},
		{name: "llm error", llm: &staticLLM{err: errors.New("rate limited")}, wantErr: true},
		{name: "invalid json", llm: &staticLLM{response: "not json"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dc := &DistilledContext{
				Text:       "the magnitude of a vector is its length",
				Vocabulary: []VocabTerm{{Term: "Vector", Definition: "a"}, {Term: "vector", Definition: "b"}},
				Sections:   []Section{{Title: "Vectors", Summary: "s1"}, {Title: "Vectors", Summary: "s2"}},
			}
			warnings, err := verifyConsistency(context.Background(), tt.llm, dc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(warnings) != len(tt.wantWarnings) {
				t.Fatalf("got %d warnings, want %d: %v", len(warnings), len(tt.wantWarnings), warnings)
			}
			for i, w := range tt.wantWarnings {
				if warnings[i] != w {
					t.Fatalf("warning %d: got %q, want %q", i, warnings[i], w)
				}
			}
		})
	}
}

func TestBuildVerifyPrompt_IncludesFields(t *testing.T) {
	t.Parallel()

	dc := &DistilledContext{
		Text:       "full chapter text goes here",
		Vocabulary: []VocabTerm{{Term: "Vector", Definition: "magnitude and direction"}},
		Sections:   []Section{{Title: "Intro", Summary: "an intro section"}},
	}
	prompt, err := buildVerifyPrompt(dc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"Vector", "magnitude and direction", "Intro", "an intro section", "full chapter text goes here"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
