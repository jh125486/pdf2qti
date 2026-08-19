package generate_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/generate"
)

type fakeLLM struct {
	response string
	err      error
	prompt   string
	schema   *distill.Schema
}

func (f *fakeLLM) Complete(_ context.Context, prompt string, schema *distill.Schema) (string, error) {
	f.prompt, f.schema = prompt, schema
	return f.response, f.err
}

func TestGenerateStage_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		stage    config.Stage
		count    int
		response string
		wantErr  string
	}{
		{name: "true false", stage: config.StageTF, count: 1, response: `{"questions":[{"text":"Sky blue?","options":[{"text":"True","is_correct":true,"match_text":""},{"text":"False","is_correct":false,"match_text":""}]}]}`},
		{name: "matching", stage: config.StageMT, count: 1, response: `{"questions":[{"text":"Match","options":[{"text":"A","is_correct":true,"match_text":"1"},{"text":"B","is_correct":true,"match_text":"2"}]}]}`},
		{name: "essay", stage: config.StageES, count: 1, response: `{"questions":[{"text":"Explain.","options":[]}]}`},
		{name: "count mismatch", stage: config.StageMC, count: 2, response: `{"questions":[]}`, wantErr: "returned 0 mc questions; want 2"},
		{name: "invalid multiple choice", stage: config.StageMC, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":true,"match_text":""}]}]}`, wantErr: "requires at least two options"},
		{name: "invalid JSON", stage: config.StageMC, count: 1, response: `{`, wantErr: "parse LLM response"},
		{name: "unknown stage", stage: "nope", count: 1, wantErr: "unsupported question stage"},
		{name: "empty source", stage: config.StageMC, count: 1, wantErr: "source text is empty"},
		{name: "negative count", stage: config.StageMC, count: -1, wantErr: "must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeLLM{response: tt.response}
			source := "Grounding material"
			if tt.name == "empty source" {
				source = ""
			}
			questions, err := generate.NewWithLLM(fake).GenerateStage(t.Context(), tt.stage, source, tt.count)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error=%v want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(questions) != tt.count {
				t.Fatalf("len=%d want=%d", len(questions), tt.count)
			}
			if fake.schema == nil || fake.schema.Name != "quiz_questions" {
				t.Fatalf("schema=%+v", fake.schema)
			}
			if !strings.Contains(fake.prompt, "Grounding material") || !strings.Contains(fake.prompt, fmt.Sprintf("%s quiz", tt.stage)) {
				t.Fatalf("prompt missing source or stage: %q", fake.prompt)
			}
			if !strings.Contains(fake.prompt, `inline math uses \\(...\\)`) {
				t.Fatalf("prompt does not require Canvas-compatible LaTeX delimiters: %q", fake.prompt)
			}
		})
	}
}

func TestGenerateStage_ZeroCountSkipsLLM(t *testing.T) {
	t.Parallel()
	fake := &fakeLLM{err: fmt.Errorf("should not be called")}
	questions, err := generate.NewWithLLM(fake).GenerateStage(t.Context(), config.StageMC, "text", 0)
	if err != nil || len(questions) != 0 {
		t.Fatalf("questions=%v err=%v", questions, err)
	}
	if fake.schema != nil {
		t.Fatal("LLM called for zero count")
	}
}

func TestNew_RequiresConfiguredOpenAI(t *testing.T) {
	t.Parallel()
	_, err := generate.New(config.Generation{}, 0)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("error=%v", err)
	}
}
