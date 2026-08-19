package distill

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// verifyConsistency checks a chunked-and-condensed chapter both for the specific inconsistency
// risk that map-reduce distillation introduces (pieces distilled independently can define the
// same term two different ways, restate a formula inconsistently, or produce near-duplicate
// sections) and for standalone factual/precision errors anywhere in the condensed text — e.g. a
// worked-example detail stated as if it were the general rule, or an algorithm description
// missing a step that changes its correctness (both observed in practice: see
// TestVerifyConsistency_Table's "worked-example detail generalized as a rule" and "missing
// algorithm step" cases).
//
// It reconciles Vocabulary and Sections in place, applies each proposed Correction to dc.Text
// (see applyCorrections), and returns any issue it found that it could not auto-resolve — either
// a genuine cross-piece contradiction with no clean single fix, or a Correction whose Find text
// didn't match dc.Text exactly once (ambiguous or already-changed) — for human review. Unmatched
// Corrections are surfaced as warnings rather than silently dropped, so a bad Find string doesn't
// quietly leave a known error in place with no trace.
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
	warnings := append([]string{}, resp.Warnings...)
	warnings = append(warnings, applyCorrections(dc, resp.Corrections)...)
	return warnings, nil
}

// applyCorrections replaces each correction's Find text in dc.Text with its Replace text, in
// place, skipping (and reporting as a warning, not silently) any correction whose Find text
// doesn't occur in dc.Text exactly once — zero occurrences means the LLM misquoted the text it
// was given, and more than one is too ambiguous to know which occurrence was meant. Deliberately
// narrow and surgical: rewriting the *entire* condensed text from an LLM response (rather than
// patching specific substrings) is exactly what caused this pipeline's original text-compression
// bug (see verifyConsistency's doc comment) — small, exact, targeted replacements avoid that
// failure mode entirely.
func applyCorrections(dc *DistilledContext, corrections []textCorrection) (skipped []string) {
	for _, c := range corrections {
		if c.Find == "" || c.Find == c.Replace {
			continue
		}
		if n := strings.Count(dc.Text, c.Find); n != 1 {
			skipped = append(skipped, fmt.Sprintf("correction not applied (found %d exact matches, need exactly 1): %q -> %q", n, truncatePreview(c.Find), truncatePreview(c.Replace)))
			continue
		}
		dc.Text = strings.Replace(dc.Text, c.Find, c.Replace, 1)
	}
	return skipped
}

// correctionPreviewLimit bounds how much of a skipped correction's Find/Replace text
// truncatePreview keeps in a warning — the prompt asks the LLM for a short clause or sentence
// (see verifyPromptTmpl), but nothing enforces that server-side, so an uncooperative response
// could otherwise dump an arbitrarily long span straight into VerificationWarnings and, from
// there, into every downstream log line that reports it (see distill.go's per-warning Warn log).
const correctionPreviewLimit = 200

// truncatePreview shortens s to at most correctionPreviewLimit runes, appending "…" when
// truncated, so a warning stays a scannable one-liner regardless of how long the LLM's
// Find/Replace text turned out to be. Scans rune boundaries via range (which decodes one rune at
// a time, not a []rune(s) conversion) and returns as soon as it reaches the limit, so an
// oversized s — the exact case this function guards against — is never fully materialized as
// runes just to measure and discard the excess; caught in PR review.
func truncatePreview(s string) string {
	count := 0
	for i := range s {
		if count == correctionPreviewLimit {
			return s[:i] + "…"
		}
		count++
	}
	return s
}

// textCorrection is a single verbatim substring of dc.Text (Find) and its corrected replacement
// (Replace), as proposed by the verify LLM call. Kept intentionally narrow — a clause or
// sentence, never a whole paragraph or section — so applyCorrections can match and replace it
// exactly rather than asking the LLM to reproduce surrounding text it might subtly alter.
type textCorrection struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// verifyResponse is the shape of the LLM's JSON response to verifyPromptTmpl.
type verifyResponse struct {
	Vocabulary  vocabList        `json:"vocabulary"`
	Sections    sectionList      `json:"sections"`
	Corrections []textCorrection `json:"corrections"`
	Warnings    []string         `json:"warnings"`
}

// verifyPromptTmpl is the LLM prompt template for the internal-consistency and correctness check.
var verifyPromptTmpl = template.Must(template.New("verify").Parse(`You are checking one already-distilled textbook chapter for internal consistency AND for
standalone factual/precision errors. The chapter was distilled in pieces (chunked and condensed
independently before this step), which can introduce duplicate or contradictory entries across
pieces — but a single piece can also contain a real error entirely on its own, with nothing else
in the text to contradict it. Both are your job to catch.

Two error shapes to watch for specifically, beyond plain contradictions:
- A worked-example detail stated as if it were the general rule (e.g. "the last row indicates a
  free variable" when that was only true for one specific example's matrix, not the definition).
- An algorithm or procedure description missing a step whose absence changes correctness (e.g.
  "find the first nonzero entry in the column" without saying "at or below the current row",
  which silently allows picking an already-used pivot from above).

## Vocabulary (as currently distilled)
{{range .Vocabulary}}- {{.Term}}: {{.Definition}}
{{end}}
## Sections (as currently distilled)
{{range .Sections}}- {{.Title}}: {{.Summary}}
{{end}}
## Full chapter text (for cross-checking and correction — do not reproduce or summarize this in
your response; quote from it exactly only inside a correction's "find" field)
{{.Text}}

## Task
Produce a JSON object with these exact fields:
- vocabulary: the vocabulary list above, with duplicate terms merged (case-insensitive) into a
  single entry using the clearest/most complete definition, and any two entries that define the
  same term differently reconciled into one correct definition
- sections: the sections list above, with duplicate or heavily overlapping sections merged into
  one (keep the more complete summary), preserving original order
- corrections: array of {"find": "...", "replace": "..."} objects, each fixing one specific error
  found anywhere in the full chapter text above (including the two error shapes described above).
  "find" MUST be an exact, verbatim substring copied character-for-character from the full chapter
  text — a short clause or sentence, never a whole paragraph — so it can be located and replaced
  automatically; if you cannot quote the exact text, do not propose that correction. "replace" is
  the corrected version of that same span, keeping everything around it valid. Empty array if
  none needed.
- warnings: array of short strings, one per issue you found but could not turn into a safe,
  exact-quote correction above (e.g. a contradiction spanning too much text to quote narrowly, or
  a formula stated two different ways in different parts of the text with no single fix); empty
  array if none

Do not include a "text" field in your response.`))

// buildVerifyPrompt renders the LLM prompt for the internal-consistency check.
func buildVerifyPrompt(dc *DistilledContext) (string, error) {
	var buf bytes.Buffer
	if err := verifyPromptTmpl.Execute(&buf, dc); err != nil {
		return "", err
	}
	return buf.String(), nil
}
