package distill

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
)

// verifyConsistency checks a chunked-and-condensed chapter for the specific inconsistency risk
// that map-reduce distillation introduces: pieces distilled independently can define the same
// term two different ways, restate a formula inconsistently, or produce near-duplicate sections.
// It reconciles Vocabulary and Sections in place and returns any contradiction it found in
// dc.Text that it could not auto-resolve, for human review. dc.Text itself is never rewritten —
// asking an LLM to reproduce it verbatim is exactly what caused the original text-compression
// bug this pipeline used to have.
func verifyConsistency(ctx context.Context, llm LLM, dc *DistilledContext) ([]string, error) {
	prompt, err := buildVerifyPrompt(dc)
	if err != nil {
		return nil, fmt.Errorf("build verify prompt: %w", err)
	}

	raw, err := llm.Complete(ctx, prompt, nil)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}

	var resp verifyResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	dc.Vocabulary = resp.Vocabulary
	dc.Sections = resp.Sections
	return resp.Warnings, nil
}

// verifyResponse is the shape of the LLM's JSON response to verifyPromptTmpl.
type verifyResponse struct {
	Vocabulary vocabList   `json:"vocabulary"`
	Sections   sectionList `json:"sections"`
	Warnings   []string    `json:"warnings"`
}

// verifyPromptTmpl is the LLM prompt template for the internal-consistency check.
var verifyPromptTmpl = template.Must(template.New("verify").Parse(`You are checking one already-distilled textbook chapter for internal consistency. The chapter
was distilled in pieces (chunked and condensed independently before this step), which can
introduce duplicate or contradictory entries across pieces — your job is to catch and fix that.

## Vocabulary (as currently distilled)
{{range .Vocabulary}}- {{.Term}}: {{.Definition}}
{{end}}
## Sections (as currently distilled)
{{range .Sections}}- {{.Title}}: {{.Summary}}
{{end}}
## Full chapter text (for cross-checking only — do not reproduce or summarize this in your response)
{{.Text}}

## Task
Produce a JSON object with these exact fields:
- vocabulary: the vocabulary list above, with duplicate terms merged (case-insensitive) into a
  single entry using the clearest/most complete definition, and any two entries that define the
  same term differently reconciled into one correct definition
- sections: the sections list above, with duplicate or heavily overlapping sections merged into
  one (keep the more complete summary), preserving original order
- warnings: array of short strings, one per unresolved contradiction you found in the full
  chapter text that the vocabulary/sections fixes above don't already cover (e.g. a formula
  stated two different ways in different parts of the text); empty array if none

Do not include a "text" field in your response.`))

// buildVerifyPrompt renders the LLM prompt for the internal-consistency check.
func buildVerifyPrompt(dc *DistilledContext) (string, error) {
	var buf bytes.Buffer
	if err := verifyPromptTmpl.Execute(&buf, dc); err != nil {
		return "", err
	}
	return buf.String(), nil
}
