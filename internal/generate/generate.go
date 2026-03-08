// Package generate provides LLM-based quiz question generation.
package generate

import (
	"context"
	"fmt"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/render"
)

// Generator generates quiz questions from source text.
type Generator struct {
	cfg config.Generation
}

// New creates a new Generator.
func New(cfg config.Generation) *Generator {
	return &Generator{cfg: cfg}
}

// GenerateStage generates questions for a specific stage.
func (g *Generator) GenerateStage(ctx context.Context, stage config.Stage, sourceText string, count int) ([]render.Question, error) {
	// In a real implementation, this would call the LLM API.
	// For now, return stub questions.
	_ = ctx
	_ = sourceText
	questions := make([]render.Question, count)
	for i := range questions {
		questions[i] = buildStubQuestion(stage, i+1)
	}
	return questions, nil
}

func buildStubQuestion(stage config.Stage, n int) render.Question {
	q := render.Question{
		Number: n,
		Text:   fmt.Sprintf("Sample %s question %d?", string(stage), n),
	}
	switch stage {
	case config.StageTF:
		q.Options = []render.Option{
			{Text: "True", IsCorrect: true},
			{Text: "False", IsCorrect: false},
		}
	case config.StageMA:
		q.Options = []render.Option{
			{Text: "Option A", IsCorrect: true},
			{Text: "Option B", IsCorrect: false},
			{Text: "Option C", IsCorrect: true},
		}
	case config.StageMC:
		q.Options = []render.Option{
			{Text: "Option A", IsCorrect: true},
			{Text: "Option B", IsCorrect: false},
			{Text: "Option C", IsCorrect: false},
		}
	}
	return q
}
