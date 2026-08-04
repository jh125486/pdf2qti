package distill

import (
	"context"
	"fmt"
	"sort"

	"github.com/jh125486/pdf2qti/internal/config"
)

// LLM is the interface for calling a language model. schema is nil for calls that want free-form
// prose (e.g. condenseChunk's digests) or whose response shape is intentionally tolerant of
// several JSON forms (e.g. DistilledContext's vocabList/sectionList); non-nil for calls whose
// response must conform to schema exactly, letting an implementation that supports it (see
// internal/openai's use of OpenAI's Structured Outputs) enforce that server-side instead of
// hoping the model followed the prompt's prose description of the shape.
type LLM interface {
	Complete(ctx context.Context, prompt string, schema *Schema) (string, error)
}

// Schema describes a JSON Schema an LLM response must conform to, for providers that support
// enforcing it server-side (OpenAI's Structured Outputs, via response_format/json_schema with
// strict:true). Name is a short identifier for the schema (required by OpenAI's API — letters,
// digits, underscores, hyphens only). Definition is the literal JSON Schema object; per OpenAI's
// strict-mode constraints, every object in it needs "additionalProperties": false and every
// property listed in "required" (use a `["type", "null"]` union for an optional field, since
// strict mode doesn't support omitting required keys).
type Schema struct {
	Name       string
	Definition map[string]any
}

// jsonSchemaTypeKey is the JSON Schema "type" keyword, shared by the schema-building helpers
// below rather than repeating the "type" string literal at every node they build.
const jsonSchemaTypeKey = "type"

// jsonSchemaString is the JSON Schema node for a plain string value, shared by every schema built
// with the helpers below rather than repeating the {"type": "string"} literal at every leaf.
var jsonSchemaString = map[string]any{jsonSchemaTypeKey: "string"}

// jsonSchemaInteger is the JSON Schema node for a plain integer value, shared the same way as
// jsonSchemaString above.
var jsonSchemaInteger = map[string]any{jsonSchemaTypeKey: "integer"}

// jsonSchemaArray builds a JSON Schema "array" node whose elements each match itemSchema.
func jsonSchemaArray(itemSchema map[string]any) map[string]any {
	return map[string]any{
		jsonSchemaTypeKey: "array",
		"items":           itemSchema,
	}
}

// jsonSchemaObject builds a JSON Schema "object" node from properties, wiring up the strict-mode
// boilerplate (see Schema's doc comment) every object in this package's schemas needs:
// "additionalProperties": false, and "required" listing every one of properties's keys — every
// field in this package's schemas is always required, so deriving "required" from properties's
// keys here means a schema can't drift out of sync with itself by adding a property and
// forgetting to also require it.
func jsonSchemaObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for k := range properties {
		required = append(required, k)
	}
	sort.Strings(required)
	return map[string]any{
		jsonSchemaTypeKey:      "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
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

	raw, err := llm.Complete(ctx, prompt, nil)
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
