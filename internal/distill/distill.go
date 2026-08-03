package distill

import (
	"context"
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
	// condensedText is non-empty only when chapterText was too large to send directly. In that
	// case it becomes the final Text field verbatim: asking the same synthesis call that
	// produces the other structured fields to ALSO reproduce this much prose is unreliable in
	// practice (models compress it far below the length actually requested), so we bypass that
	// and use the condensed map-reduce material directly instead.
	var condensedText string
	promptText := chapterText
	if len(chapterText) > maxDirectChars {
		condensed, err := condenseChunks(ctx, llm, chapterText)
		if err != nil {
			return nil, fmt.Errorf("condense chapter text: %w", err)
		}
		condensedText = condensed
		promptText = condensed
	}

	prompt, err := buildPrompt(objectives, promptText)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	var dc DistilledContext
	if err := unmarshalRepaired(raw, &dc); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	if condensedText != "" {
		dc.Text = condensedText

		// Chunked distillation is the only case with cross-piece inconsistency risk (each piece
		// was condensed independently); a single-call chapter has nothing to reconcile.
		warnings, err := verifyConsistency(ctx, llm, &dc)
		if err != nil {
			return nil, fmt.Errorf("verify consistency: %w", err)
		}
		dc.VerificationWarnings = warnings
	}

	// Populate fields from config that the LLM doesn't set.
	dc.SourceID = src.ID
	dc.Book = src.Name
	dc.Chapter = src.Chapter

	return &dc, nil
}
