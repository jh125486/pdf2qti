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

func (s *staticLLM) Complete(_ context.Context, _ string, _ *Schema) (string, error) {
	return s.response, s.err
}

func TestVerifyConsistency_Table(t *testing.T) {
	t.Parallel()

	const baseText = "the magnitude of a vector is its length. the last row indicates a free variable in this case."

	tests := []struct {
		name         string
		text         string
		llm          *staticLLM
		wantErr      bool
		wantWarnings []string
		wantText     string // "" means "unchanged from input text"
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
		{
			// A worked-example detail stated as if it were the general rule — exactly the ch03
			// regression this feature exists to fix (see verifyConsistency's doc comment).
			name: "worked-example detail generalized as a rule",
			llm: &staticLLM{response: `{
				"vocabulary": [],
				"sections": [],
				"corrections": [{
					"find": "the last row indicates a free variable in this case",
					"replace": "in this particular example, the last row happens to correspond to the free variable; in general, free variables are identified by nonpivot coefficient columns"
				}],
				"warnings": []
			}`},
			wantText: "the magnitude of a vector is its length. in this particular example, the last row happens to correspond to the free variable; in general, free variables are identified by nonpivot coefficient columns.",
		},
		{
			// An algorithm description missing a qualifying step — the other ch03 regression.
			name: "missing algorithm step",
			text: "the pivot is the first nonzero entry in the column.",
			llm: &staticLLM{response: `{
				"vocabulary": [],
				"sections": [],
				"corrections": [{
					"find": "the first nonzero entry in the column",
					"replace": "the first nonzero entry in the column at or below the current pivot row"
				}],
				"warnings": []
			}`},
			wantText: "the pivot is the first nonzero entry in the column at or below the current pivot row.",
		},
		{
			name: "correction skipped when find text is not present verbatim",
			llm: &staticLLM{response: `{
				"vocabulary": [], "sections": [],
				"corrections": [{"find": "text that does not appear anywhere", "replace": "irrelevant"}],
				"warnings": []
			}`},
			wantWarnings: []string{`correction not applied (found 0 exact matches, need exactly 1): "text that does not appear anywhere" -> "irrelevant"`},
		},
		{
			name: "correction skipped when find text is ambiguous",
			text: "repeat repeat repeat",
			llm: &staticLLM{response: `{
				"vocabulary": [], "sections": [],
				"corrections": [{"find": "repeat", "replace": "once"}],
				"warnings": []
			}`},
			wantWarnings: []string{`correction not applied (found 3 exact matches, need exactly 1): "repeat" -> "once"`},
			wantText:     "repeat repeat repeat",
		},
		{
			name: "no-op correction (find equals replace) is silently skipped",
			llm: &staticLLM{response: `{
				"vocabulary": [], "sections": [],
				"corrections": [{"find": "the magnitude of a vector is its length", "replace": "the magnitude of a vector is its length"}],
				"warnings": []
			}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text := tt.text
			if text == "" && !tt.wantErr {
				text = baseText
			}
			dc := &DistilledContext{
				Text:       text,
				Vocabulary: []VocabTerm{{Term: "Vector", Definition: "a"}, {Term: "vector", Definition: "b"}},
				Sections:   []Section{{Title: "Vectors", Summary: "s1"}, {Title: "Vectors", Summary: "s2"}},
			}
			warnings, err := verifyConsistency(t.Context(), tt.llm, dc)
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
			wantText := tt.wantText
			if wantText == "" {
				wantText = text
			}
			if dc.Text != wantText {
				t.Fatalf("got text %q, want %q", dc.Text, wantText)
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
	for _, want := range []string{"Vector", "magnitude and direction", "Intro", "an intro section", "full chapter text goes here", "corrections", "find", "replace"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
