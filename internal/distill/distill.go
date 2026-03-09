package distill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jh125486/pdf2qti/internal/config"
)

// LLM is the interface for calling a language model.
type LLM interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Distill calls the LLM to produce a DistilledContext for the given source.
// chapterText is the raw text extracted from the source PDF.
func Distill(ctx context.Context, src *config.Source, objectives []config.CourseObjective, llm LLM, chapterText string) (*DistilledContext, error) {
	prompt, err := buildPrompt(objectives, chapterText)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	var dc DistilledContext
	if err := json.Unmarshal([]byte(raw), &dc); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	// Populate fields from config that the LLM doesn't set.
	dc.SourceID = src.ID
	dc.Book = src.Name
	dc.Chapter = src.Chapter

	return &dc, nil
}
