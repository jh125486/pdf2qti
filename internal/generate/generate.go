// Package generate provides LLM-based quiz question generation.
package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/openai"
	"github.com/jh125486/pdf2qti/internal/render"
)

// LLM is generation's small dependency on a model client. OpenAI's Client implements it.
// Kept here so tests can use deterministic fakes without network access.
type LLM interface {
	Complete(context.Context, string, *distill.Schema) (string, error)
}

// Generator generates quiz questions from source text.
type Generator struct {
	llm LLM
}

const schemaTypeKey = "type"

// New creates an OpenAI-backed generator from resolved generation config. It never falls back to
// placeholder questions: a missing key or unsupported provider is a configuration error.
func New(cfg config.Generation, httpTimeout time.Duration) (*Generator, error) { //nolint:gocritic // matches config.Generation-by-value convention used elsewhere
	llm, err := openai.New(cfg, httpTimeout)
	if err != nil {
		return nil, fmt.Errorf("create generation LLM: %w", err)
	}
	return NewWithLLM(llm), nil
}

// NewWithLLM creates a generator with llm. Intended for embedding and deterministic tests.
func NewWithLLM(llm LLM) *Generator { return &Generator{llm: llm} }

type stageResponse struct {
	Questions []generatedQuestion `json:"questions"`
}

type generatedQuestion struct {
	Text    string            `json:"text"`
	Options []generatedOption `json:"options"`
}

type generatedOption struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
	MatchText string `json:"match_text"`
}

// GenerateStage generates exactly count questions for stage, grounded only in sourceText.
func (g *Generator) GenerateStage(ctx context.Context, stage config.Stage, sourceText string, count int) ([]render.Question, error) {
	if count < 0 {
		return nil, fmt.Errorf("question count must not be negative")
	}
	if count == 0 {
		return nil, nil
	}
	if !validStage(stage) {
		return nil, fmt.Errorf("unsupported question stage %q", stage)
	}
	if strings.TrimSpace(sourceText) == "" {
		return nil, fmt.Errorf("source text is empty")
	}
	if g == nil || g.llm == nil {
		return nil, fmt.Errorf("generation LLM is not configured")
	}

	raw, err := g.llm.Complete(ctx, buildPrompt(stage, sourceText, count), questionSchema())
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var response stageResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}
	if len(response.Questions) != count {
		return nil, fmt.Errorf("LLM returned %d %s questions; want %d", len(response.Questions), stage, count)
	}

	questions := make([]render.Question, len(response.Questions))
	for i, question := range response.Questions {
		if err := validateQuestion(stage, question); err != nil {
			return nil, fmt.Errorf("%s question %d: %w", stage, i+1, err)
		}
		options := make([]render.Option, len(question.Options))
		for j, option := range question.Options {
			options[j] = render.Option{Text: option.Text, IsCorrect: option.IsCorrect, MatchText: option.MatchText}
		}
		questions[i] = render.Question{Number: i + 1, Text: question.Text, Options: options}
	}
	return questions, nil
}

func validStage(stage config.Stage) bool {
	switch stage {
	case config.StageTF, config.StageMA, config.StageMC, config.StageSA, config.StageES, config.StageMT, config.StageNR:
		return true
	default:
		return false
	}
}

func validateQuestion(stage config.Stage, question generatedQuestion) error { //nolint:gocyclo // stage-specific QTI shapes are clearest together
	if strings.TrimSpace(question.Text) == "" {
		return fmt.Errorf("text is empty")
	}
	correct := 0
	for i, option := range question.Options {
		if strings.TrimSpace(option.Text) == "" {
			return fmt.Errorf("option %d text is empty", i+1)
		}
		if option.IsCorrect {
			correct++
		}
	}
	switch stage {
	case config.StageTF:
		if len(question.Options) != 2 || correct != 1 {
			return fmt.Errorf("requires exactly two options and one correct answer")
		}
	case config.StageMC:
		if len(question.Options) < 2 || correct != 1 {
			return fmt.Errorf("requires at least two options and one correct answer")
		}
	case config.StageMA:
		if len(question.Options) < 2 || correct == 0 {
			return fmt.Errorf("requires at least two options and one correct answer")
		}
	case config.StageSA:
		if len(question.Options) == 0 || correct != len(question.Options) {
			return fmt.Errorf("requires one or more accepted answers")
		}
	case config.StageES:
		if len(question.Options) != 0 {
			return fmt.Errorf("must not have options")
		}
	case config.StageMT:
		if len(question.Options) < 2 || correct != len(question.Options) {
			return fmt.Errorf("requires at least two matching pairs")
		}
		for i, option := range question.Options {
			if strings.TrimSpace(option.MatchText) == "" {
				return fmt.Errorf("match %d right side is empty", i+1)
			}
		}
	case config.StageNR:
		if len(question.Options) == 0 || correct != 1 {
			return fmt.Errorf("requires one exact numeric answer")
		}
	}
	return nil
}

func buildPrompt(stage config.Stage, sourceText string, count int) string {
	return fmt.Sprintf(`Create exactly %d %s quiz questions from source material below.

Ground every question and answer in source material. Do not invent facts. Return JSON only, matching supplied schema. Write every mathematical expression in LaTeX delimiters: inline math uses \\(...\\) and display math uses \\[...\\]. Preserve source LaTeX exactly when it already uses these delimiters. For unused match_text fields return an empty string.

%s

## Source material
%s`, count, stage, stageInstructions(stage), sourceText)
}

func stageInstructions(stage config.Stage) string {
	switch stage {
	case config.StageTF:
		return "Each question: exactly two options, exactly one correct. Use True and False options."
	case config.StageMA:
		return "Each question: at least two options, one or more correct."
	case config.StageMC:
		return "Each question: at least two options, exactly one correct."
	case config.StageSA:
		return "Each question: one or more accepted answers; mark all options correct."
	case config.StageES:
		return "Each question: open-ended essay prompt; options must be empty."
	case config.StageMT:
		return "Each question: at least two matching pairs; option text is left side and match_text is right side; mark all correct."
	case config.StageNR:
		return "Each question: one exact numeric answer marked correct; optional tolerance options marked incorrect."
	default:
		return ""
	}
}

func questionSchema() *distill.Schema {
	stringSchema := map[string]any{schemaTypeKey: "string"}
	optionSchema := strictObject(map[string]any{
		"text":       stringSchema,
		"is_correct": map[string]any{schemaTypeKey: "boolean"},
		"match_text": stringSchema,
	})
	question := strictObject(map[string]any{
		"text":    stringSchema,
		"options": map[string]any{schemaTypeKey: "array", "items": optionSchema},
	})
	return &distill.Schema{Name: "quiz_questions", Definition: strictObject(map[string]any{
		"questions": map[string]any{schemaTypeKey: "array", "items": question},
	})}
}

func strictObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	return map[string]any{schemaTypeKey: "object", "properties": properties, "required": required, "additionalProperties": false}
}
