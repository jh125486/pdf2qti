package distill

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
)

// reviewDeck asks llm to review an already-finalized deck's full markdown for quality issues a
// human would want to know about before presenting it: factually or mathematically incorrect
// content, duplicate/near-duplicate slides covering the same material, thin or empty-feeling
// slides, and broken or garbled LaTeX/formatting. Each issue becomes one warning string; an empty
// review (no issues) returns a nil/empty slice, not an error.
//
// This is advisory quality feedback on content that's already finalized, not a required step in
// producing the deck — the caller (GenerateProtoDeck) is expected to treat a failure here as
// non-fatal (log/warn and return the deck anyway), unlike every other LLM call in this package,
// whose output is literally part of the deck. See GenerateProtoDeck's call site for why.
func reviewDeck(ctx context.Context, llm LLM, deck string) ([]string, error) {
	prompt, err := buildReviewDeckPrompt(deck)
	if err != nil {
		return nil, fmt.Errorf("build review prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, reviewDeckSchema)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp reviewDeckResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	if len(resp.Issues) == 0 {
		return nil, nil
	}
	warnings := make([]string, len(resp.Issues))
	for i, issue := range resp.Issues {
		warnings[i] = fmt.Sprintf("[%s] %s: %s", issue.Severity, issue.SlideTitle, issue.Description)
	}
	return warnings, nil
}

// reviewIssue is one entry in reviewDeckResponse.
type reviewIssue struct {
	Severity    string `json:"severity"`
	SlideTitle  string `json:"slide_title"`
	Description string `json:"description"`
}

// reviewDeckResponse is the shape of the LLM's JSON response to reviewDeckPromptTmpl.
type reviewDeckResponse struct {
	Issues []reviewIssue `json:"issues"`
}

// reviewDeckSchema is reviewDeckResponse's JSON Schema, enforced server-side for providers that
// support it (see Schema's doc comment).
var reviewDeckSchema = &Schema{
	Name: "deck_review",
	Definition: jsonSchemaObject(map[string]any{
		"issues": jsonSchemaArray(jsonSchemaObject(map[string]any{
			"severity":    jsonSchemaString,
			"slide_title": jsonSchemaString,
			"description": jsonSchemaString,
		})),
	}),
}

// reviewDeckPromptTmpl is the LLM prompt template for reviewing a finalized deck's quality.
var reviewDeckPromptTmpl = template.Must(template.New("reviewdeck").Parse(`You are reviewing a finished prototype PowerPoint deck (below, in full) for quality issues before
it's presented in a lecture. This deck has already been finalized — you are reviewing it, not
rewriting it.

## Deck
{{.Deck}}

## Task
Look for:
- Mathematically or factually incorrect content — a wrong formula, an incorrect worked-example
  result, a definition that contradicts itself elsewhere in the deck.
- Duplicate or near-duplicate slides covering the same specific content twice, even under
  different titles.
- Thin or empty-feeling slides that don't teach anything concrete.
- Broken or garbled LaTeX/formatting that would render incorrectly.

Produce a JSON object: {"issues": [{"severity": "...", "slide_title": "...", "description": "..."}]}.

- severity: one of "high", "medium", "low".
- slide_title: the exact title of the slide the issue is on (the text after the "# " heading), or
  the first affected slide's title if an issue spans two slides (e.g. a duplicate pair).
- description: one concise sentence describing the specific issue — enough for someone to find and
  fix it without re-deriving what's wrong.

If you find nothing worth flagging, return {"issues": []} — do not invent issues to have something
to report.
`))

type reviewDeckPromptData struct {
	Deck string
}

// buildReviewDeckPrompt renders the LLM prompt for reviewing a finalized deck.
func buildReviewDeckPrompt(deck string) (string, error) {
	var buf bytes.Buffer
	if err := reviewDeckPromptTmpl.Execute(&buf, reviewDeckPromptData{Deck: deck}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
