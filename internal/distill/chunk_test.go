// Whitebox package: splitIntoChunks and condenseChunks are unexported (only the Distill entry
// point needs to be public), so their tests live alongside the production code.
package distill

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSplitIntoChunks_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		size       int
		wantChunks int
	}{
		{name: "empty text", text: "", size: 100, wantChunks: 0},
		{name: "single small paragraph", text: "hello world", size: 100, wantChunks: 1},
		{name: "paragraphs combine under size", text: "aaaa\n\nbbbb", size: 100, wantChunks: 1},
		{
			name:       "paragraphs forced apart",
			text:       strings.Repeat("a", 60) + "\n\n" + strings.Repeat("b", 60),
			size:       100,
			wantChunks: 2,
		},
		{name: "oversized paragraph hard split", text: strings.Repeat("x", 250), size: 100, wantChunks: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chunks := splitIntoChunks(tt.text, tt.size)
			if len(chunks) != tt.wantChunks {
				t.Fatalf("got %d chunks, want %d: %#v", len(chunks), tt.wantChunks, chunks)
			}
			for _, c := range chunks {
				if len(c) > tt.size {
					t.Fatalf("chunk exceeds size %d: %d bytes", tt.size, len(c))
				}
			}
		})
	}
}

type recordingLLM struct {
	prompts []string
	err     error
}

func (r *recordingLLM) Complete(_ context.Context, prompt string, _ *Schema) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	r.prompts = append(r.prompts, prompt)
	return "digest", nil
}

func TestCondenseChunks_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		size      int
		llmErr    error
		wantErr   bool
		wantCalls int
	}{
		{name: "single chunk", text: "short chapter text", size: 100, wantCalls: 1},
		{
			name:      "multiple chunks",
			text:      strings.Repeat("a", 50) + "\n\n" + strings.Repeat("b", 50) + "\n\n" + strings.Repeat("c", 50),
			size:      100,
			wantCalls: 3,
		},
		{name: "llm error propagates", text: "text", size: 100, llmErr: errors.New("rate limited"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm := &recordingLLM{err: tt.llmErr}
			got, err := condenseChunksSize(t.Context(), llm, tt.text, tt.size)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(llm.prompts) != tt.wantCalls {
				t.Fatalf("got %d LLM calls, want %d", len(llm.prompts), tt.wantCalls)
			}
			if got == "" {
				t.Fatal("expected non-empty condensed text")
			}
		})
	}
}
