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

func TestGenerateStage_Table(t *testing.T) { //nolint:gocyclo // table covers each stage's validation contract
	t.Parallel()
	tests := []struct {
		name     string
		stage    config.Stage
		count    int
		response string
		wantErr  string
		source   string
		llmErr   error
		nilGen   bool
		nilLLM   bool
	}{
		{name: "true false", stage: config.StageTF, count: 1, response: `{"questions":[{"text":"Sky blue?","options":[{"text":"True","is_correct":true,"match_text":""},{"text":"False","is_correct":false,"match_text":""}]}]}`},
		{name: "multiple answer", stage: config.StageMA, count: 1, response: `{"questions":[{"text":"Pick colors","options":[{"text":"Red","is_correct":true,"match_text":""},{"text":"Blue","is_correct":true,"match_text":""}]}]}`},
		{name: "multiple choice", stage: config.StageMC, count: 1, response: `{"questions":[{"text":"Two plus two?","options":[{"text":"4","is_correct":true,"match_text":""},{"text":"5","is_correct":false,"match_text":""}]}]}`},
		{name: "short answer", stage: config.StageSA, count: 1, response: `{"questions":[{"text":"Name water","options":[{"text":"H2O","is_correct":true,"match_text":""}]}]}`},
		{name: "matching", stage: config.StageMT, count: 1, response: `{"questions":[{"text":"Match","options":[{"text":"A","is_correct":true,"match_text":"1"},{"text":"B","is_correct":true,"match_text":"2"}]}]}`},
		{name: "essay", stage: config.StageES, count: 1, response: `{"questions":[{"text":"Explain.","options":[]}]}`},
		{name: "numeric response", stage: config.StageNR, count: 1, response: `{"questions":[{"text":"Two plus two?","options":[{"text":"4","is_correct":true,"match_text":""}]}]}`},
		{name: "count mismatch", stage: config.StageMC, count: 2, response: `{"questions":[]}`, wantErr: "returned 0 mc questions; want 2"},
		{name: "invalid multiple choice", stage: config.StageMC, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":true,"match_text":""}]}]}`, wantErr: "requires at least two options"},
		{name: "empty question text", stage: config.StageMC, count: 1, response: `{"questions":[{"text":" ","options":[{"text":"A","is_correct":true,"match_text":""},{"text":"B","is_correct":false,"match_text":""}]}]}`, wantErr: "text is empty"},
		{name: "empty option text", stage: config.StageMC, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":" ","is_correct":true,"match_text":""},{"text":"B","is_correct":false,"match_text":""}]}]}`, wantErr: "option 1 text is empty"},
		{name: "true false wrong correct count", stage: config.StageTF, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"True","is_correct":false,"match_text":""},{"text":"False","is_correct":false,"match_text":""}]}]}`, wantErr: "requires exactly two options"},
		{name: "multiple answer no correct", stage: config.StageMA, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":false,"match_text":""},{"text":"B","is_correct":false,"match_text":""}]}]}`, wantErr: "requires at least two options and one correct"},
		{name: "short answer not all correct", stage: config.StageSA, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":true,"match_text":""},{"text":"B","is_correct":false,"match_text":""}]}]}`, wantErr: "requires one or more accepted answers"},
		{name: "essay has options", stage: config.StageES, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":false,"match_text":""}]}]}`, wantErr: "must not have options"},
		{name: "matching too few", stage: config.StageMT, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":true,"match_text":"1"}]}]}`, wantErr: "requires at least two matching pairs"},
		{name: "matching right side empty", stage: config.StageMT, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"A","is_correct":true,"match_text":""},{"text":"B","is_correct":true,"match_text":"2"}]}]}`, wantErr: "match 1 right side is empty"},
		{name: "numeric no correct", stage: config.StageNR, count: 1, response: `{"questions":[{"text":"Q","options":[{"text":"4","is_correct":false,"match_text":""}]}]}`, wantErr: "requires one exact numeric answer"},
		{name: "invalid JSON", stage: config.StageMC, count: 1, response: `{`, wantErr: "parse LLM response"},
		{name: "unknown stage", stage: "nope", count: 1, wantErr: "unsupported question stage"},
		{name: "empty source", stage: config.StageMC, count: 1, source: " ", wantErr: "source text is empty"},
		{name: "negative count", stage: config.StageMC, count: -1, wantErr: "must not be negative"},
		{name: "nil generator", stage: config.StageMC, count: 1, nilGen: true, wantErr: "generation LLM is not configured"},
		{name: "nil LLM", stage: config.StageMC, count: 1, nilLLM: true, wantErr: "generation LLM is not configured"},
		{name: "LLM error", stage: config.StageMC, count: 1, llmErr: fmt.Errorf("model unavailable"), response: `{"questions":[]}`, wantErr: "llm complete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeLLM{response: tt.response, err: tt.llmErr}
			source := "Grounding material"
			if tt.source != "" {
				source = tt.source
			}
			var generator *generate.Generator
			switch {
			case tt.nilGen:
				generator = nil
			case tt.nilLLM:
				generator = generate.NewWithLLM(nil)
			default:
				generator = generate.NewWithLLM(fake)
			}
			questions, err := generator.GenerateStage(t.Context(), tt.stage, source, tt.count)
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

func TestNew_WithConfiguredOpenAI(t *testing.T) { //nolint:gosec // test-only credential fixture
	// Non-parallel: t.Setenv mutates process environment for this constructor test.
	t.Setenv("PDF2QTI_TEST_OPENAI_KEY", "sk-test")
	generator, err := generate.New(config.Generation{ //nolint:gosec // test-only credential fixture
		Provider:  "openai",
		APIKeyEnv: "PDF2QTI_TEST_OPENAI_KEY",
	}, 0)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if generator == nil {
		t.Fatal("New() returned nil generator")
	}
}
