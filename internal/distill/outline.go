package distill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// outlineEntry is one planned content slide: which chapter it belongs to, its title, a short
// description of what it should cover, and which of that chapter's chunks (see outlineChunkChars
// in outline_chunks.go) it was planned from. ChunkIndices lets expandBatch (below) ground each
// batch's prompt on just the source text those specific entries came from, instead of a whole
// chapter's text resent on every batch call — see groundingText. It's a slice, not a single index,
// because reconcileOutline can merge two entries from different chunks into one; every entry
// generateChunkOutline creates directly has exactly one.
type outlineEntry struct {
	Tag          string `json:"tag"`
	Title        string `json:"title"`
	Focus        string `json:"focus"`
	ChunkIndices []int  `json:"chunk_indices"`
}

// UnmarshalJSON accepts the normal {tag,title,focus} object shape, and also tolerates a bare
// string in its place (treated as a title-only entry, Tag left empty) — observed in practice from
// reconcileOutlinePromptTmpl's response, where the model occasionally echoes an entry back as a
// plain string instead of the requested object. reconcileOutline's existing knownTags check then
// safely drops a title-only entry (an empty Tag never matches a known chapter tag) with a
// warning, instead of the whole reconcile response failing to parse over one malformed entry.
func (e *outlineEntry) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var title string
		if err := json.Unmarshal(data, &title); err != nil {
			return fmt.Errorf("outline entry: expected object or string: %w", err)
		}
		e.Title = title
		return nil
	}
	type alias outlineEntry
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*e = outlineEntry(a)
	return nil
}

// outlineResponse is the shape GenerateProtoDeck assembles from generateOutlineChunked's and
// generateAgenda's separate results, in the same shape the old single-call outline response used
// to have — so expandOutline (below) doesn't need to know or care that planning is now chunked.
type outlineResponse struct {
	DeckTitle string         `json:"deck_title"`
	Agenda    []string       `json:"agenda"`
	Outline   []outlineEntry `json:"outline"`
}

// expandBatchSize is how many outline entries are expanded into full bullets per LLM call —
// small enough that each call's completion is a manageable, reliably-grounded size, large enough
// to keep the total number of calls (and therefore cost/latency) reasonable for a 30+ slide deck.
const expandBatchSize = 6

// expandOutline expands outline's entries into full bullet content, batching expandBatchSize
// entries per LLM call (each grounded in its chapter's condensed text), and assembles the final
// proto-deck markdown deterministically — titles, tags, and meta numbering come from outline and
// code, not a second LLM guess at reproducing them.
func expandOutline(ctx context.Context, llm LLM, chapters []ProtoChapterInput, outline *outlineResponse) (string, error) {
	chapterByTag := make(map[string]ProtoChapterInput, len(chapters))
	for i := range chapters {
		chapterByTag[chapters[i].Tag] = chapters[i]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n---\n\n", outline.DeckTitle)

	n := 1
	fmt.Fprintf(&b, "<!-- meta: %d agenda -->\n# Agenda\n\n", n)
	for _, item := range outline.Agenda {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	b.WriteString("\n---\n\n")
	n++

	for start := 0; start < len(outline.Outline); start += expandBatchSize {
		end := start + expandBatchSize
		if end > len(outline.Outline) {
			end = len(outline.Outline)
		}
		batch := outline.Outline[start:end]

		bullets, err := expandBatch(ctx, llm, chapterByTag, batch)
		if err != nil {
			return "", fmt.Errorf("expand slides %d-%d: %w", start+1, end, err)
		}
		if len(bullets) != len(batch) {
			return "", fmt.Errorf("expand slides %d-%d: got %d bullet sets, want %d", start+1, end, len(bullets), len(batch))
		}

		for i, entry := range batch {
			fmt.Fprintf(&b, "<!-- meta: %d %s -->\n# %s\n\n", n, entry.Tag, entry.Title)
			for _, bullet := range bullets[i] {
				fmt.Fprintf(&b, "- %s\n", bullet)
			}
			b.WriteString("\n---\n\n")
			n++
		}
	}

	summaryBullets, err := expandSummary(ctx, llm, chapters, outline.Agenda)
	if err != nil {
		return "", fmt.Errorf("expand summary: %w", err)
	}
	fmt.Fprintf(&b, "<!-- meta: %d summary -->\n# Summary\n\n", n)
	for _, bullet := range summaryBullets {
		fmt.Fprintf(&b, "- %s\n", bullet)
	}

	return b.String(), nil
}

// expandSlide is the shape of one entry in expandBatchResponse.
type expandSlide struct {
	Bullets []string `json:"bullets"`
}

// expandBatchResponse is the shape of the LLM's JSON response to expandBatchPromptTmpl.
type expandBatchResponse struct {
	Slides []expandSlide `json:"slides"`
}

// expandBatchSchema is expandBatchResponse's JSON Schema, enforced server-side for providers that
// support it (see Schema's doc comment) — this is one of the call sites that was, in practice
// against the real OpenAI API, unreliable without it: the model would occasionally prepend prose
// to the JSON or otherwise deviate from the requested shape despite the prompt's instructions.
var expandBatchSchema = &Schema{
	Name: "slide_bullets_batch",
	Definition: jsonSchemaObject(map[string]any{
		"slides": jsonSchemaArray(jsonSchemaObject(map[string]any{
			"bullets": jsonSchemaArray(jsonSchemaString),
		})),
	}),
}

// expandBatchMaxAttempts bounds expandBatch's retry loop, for two distinct failure modes
// observed in practice against the real OpenAI API: (1) expandBatchSchema requires a "slides"
// array but (schema strictness doesn't extend to array length) doesn't require it be non-empty,
// so a model can occasionally return a structurally valid but empty response for a batch it
// should have populated; (2) the model's completion content can be truncated mid-JSON (a genuine
// parse failure, not a schema violation — the outer HTTP response is complete and valid, but the
// inner content string itself cuts off, most likely a token-limit truncation on an unusually
// dense batch). Retrying the exact same prompt once resolves both in every real-API case seen so
// far, so this stays small; if it's ever exhausted, that's treated as a real error rather than
// retried indefinitely.
const expandBatchMaxAttempts = 2

// expandBatch writes bullet content for batch's outline entries in one LLM call, grounded in the
// source text of every chapter tag referenced in batch. Retries (see expandBatchMaxAttempts) if
// the response doesn't parse at all, or parses but returns fewer slide entries than batch has,
// since both are otherwise indistinguishable from a real content failure to expandOutline's
// caller.
func expandBatch(ctx context.Context, llm LLM, chapterByTag map[string]ProtoChapterInput, batch []outlineEntry) ([][]string, error) {
	prompt, err := buildExpandBatchPrompt(chapterByTag, batch)
	if err != nil {
		return nil, fmt.Errorf("build expand prompt: %w", err)
	}

	var out [][]string
	var lastErr error
	for attempt := 1; attempt <= expandBatchMaxAttempts; attempt++ {
		raw, err := llm.Complete(ctx, prompt, expandBatchSchema)
		if err != nil {
			return nil, fmt.Errorf("llm complete: %w", err)
		}
		var resp expandBatchResponse
		if err := unmarshalRepaired(raw, &resp); err != nil {
			lastErr = fmt.Errorf("parse llm response: %w", err)
			continue
		}
		lastErr = nil
		out = make([][]string, len(resp.Slides))
		for i, s := range resp.Slides {
			bullets := make([]string, len(s.Bullets))
			for j, bullet := range s.Bullets {
				bullets[j] = repairMatrixRowSeparators(repairNulBytes(bullet))
			}
			out[i] = bullets
		}
		if len(out) == len(batch) {
			return out, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// expandBatchPromptTmpl is the LLM prompt template for writing bullet content for a batch of
// already-planned slide topics.
var expandBatchPromptTmpl = template.Must(template.New("expandbatch").Parse(`You are writing the bullet content for {{len .Batch}} planned PowerPoint slides, using the
source chapter material below as your ONLY source of facts — do not invent content not grounded
in it.

## Source material
{{range .Chapters}}### {{.Tag}}: {{.ModuleName}}
{{.Text}}

{{end}}
## Planned slides
{{range $i, $e := .Batch}}{{$i}}. [{{$e.Tag}}] {{$e.Title}} — {{$e.Focus}}
{{end}}
Ignore any "Participation Activity", "Animation content/captions", "Static figure" caption, or
"Check"/"Show answer" text in the source material above — these are leftover interactive-widget
artifacts, not real content. Also ignore any "Lab"/"Python Lab" hands-on coding-exercise content —
labs are a separate assignment, not lecture material.

## Task
Produce a JSON object: {"slides": [{"bullets": ["...", "..."]}]}, one entry per planned slide
above, IN THE SAME ORDER. Each entry's bullets is an array of 5-8 strings covering exactly what
that slide's focus describes, normal weight, wrapping only key vocabulary/terms in **bold**
inline where they first appear — never bold a whole bullet.

Each bullet is a short phrase or fragment, NOT a full flowing sentence — 11 words or fewer
(formulas don't count toward this). Long sentences don't work on slides; cut to the essential
words, the way a real lecture slide would, not the way a paragraph would.

A variable referenced inline (like x_1, x_2) must use the same LaTeX math delimiters as every
other formula in your bullets, never bare underscore notation outside math mode.
`))

type expandBatchPromptData struct {
	Chapters []ProtoChapterInput
	Batch    []outlineEntry
}

// buildExpandBatchPrompt renders the LLM prompt for writing a batch of planned slides' content,
// including only the chapters actually referenced by batch (not every chapter in the module), and
// only each of those chapters' text that this batch's own entries were actually planned from (see
// groundingText) rather than the chapter's full text — the latter, resent on every one of a
// chapter's several expand-batch calls, was a substantial source of redundant, wasted input
// tokens across a full generation.
func buildExpandBatchPrompt(chapterByTag map[string]ProtoChapterInput, batch []outlineEntry) (string, error) {
	seen := make(map[string]bool, len(batch))
	var chapters []ProtoChapterInput
	for _, e := range batch {
		if seen[e.Tag] {
			continue
		}
		seen[e.Tag] = true
		c, ok := chapterByTag[e.Tag]
		if !ok {
			continue
		}
		var tagEntries []outlineEntry
		for _, be := range batch {
			if be.Tag == e.Tag {
				tagEntries = append(tagEntries, be)
			}
		}
		c.Text = groundingText(c.Text, tagEntries)
		chapters = append(chapters, c)
	}
	var buf bytes.Buffer
	if err := expandBatchPromptTmpl.Execute(&buf, expandBatchPromptData{Chapters: chapters, Batch: batch}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// groundingText returns just the chunks of text that entries were actually planned from (the
// union of their ChunkIndices, deduped and in ascending order), instead of text in full — chunks
// are recomputed via splitIntoChunks rather than stored separately, since outlineChunkChars is a
// package constant and a chapter's Text doesn't change within one GenerateProtoDeck call, so
// recomputing always reproduces the exact chunks entries were planned against.
//
// Falls back to text unchanged if any entry has no recorded ChunkIndices, or references an index
// outside the recomputed chunks — both should never happen in practice, but grounding on too much
// text is a far safer failure mode than silently grounding on too little (or none), which risks
// empty or hallucinated bullet content.
func groundingText(text string, entries []outlineEntry) string {
	chunks := splitIntoChunks(text, outlineChunkChars)
	indexSet := make(map[int]bool)
	for _, e := range entries {
		if len(e.ChunkIndices) == 0 {
			return text
		}
		for _, idx := range e.ChunkIndices {
			indexSet[idx] = true
		}
	}
	sorted := make([]int, 0, len(indexSet))
	for idx := range indexSet {
		sorted = append(sorted, idx)
	}
	sort.Ints(sorted)

	parts := make([]string, 0, len(sorted))
	for _, idx := range sorted {
		if idx < 1 || idx > len(chunks) {
			return text
		}
		parts = append(parts, chunks[idx-1])
	}
	return strings.Join(parts, "\n\n")
}

// expandSummary writes the deck's closing summary bullets: one fuller recap per agenda item,
// rather than one generic takeaway per chapter — for a single-chapter deck, "one per chapter"
// collapses to just one or two bullets total, far too thin for a real closing slide.
func expandSummary(ctx context.Context, llm LLM, chapters []ProtoChapterInput, agenda []string) ([]string, error) {
	prompt, err := buildSummaryPrompt(chapters, agenda)
	if err != nil {
		return nil, fmt.Errorf("build summary prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, summaryBulletsSchema)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp struct {
		Bullets []string `json:"bullets"`
	}
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	if len(resp.Bullets) == 0 {
		return nil, errors.New("summary has no bullets")
	}
	bullets := make([]string, len(resp.Bullets))
	for i, bullet := range resp.Bullets {
		bullets[i] = repairMatrixRowSeparators(repairNulBytes(bullet))
	}
	return bullets, nil
}

// summaryBulletsSchema is expandSummary's response JSON Schema, enforced server-side for
// providers that support it (see Schema's doc comment and expandBatchSchema).
var summaryBulletsSchema = &Schema{
	Name: "summary_bullets",
	Definition: jsonSchemaObject(map[string]any{
		"bullets": jsonSchemaArray(jsonSchemaString),
	}),
}

// summaryPromptTmpl is the LLM prompt template for the deck's closing summary bullets.
var summaryPromptTmpl = template.Must(template.New("summary").Parse(`Produce a JSON object {"bullets": ["...", "..."]}: exactly one summary bullet per agenda item
below, in the same order, turning each short agenda phrase into a slightly fuller takeaway
grounded in the chapter material — more informative than the bare agenda phrase, but still a
short phrase or fragment, NOT a full sentence: 11 words or fewer. Long, flowing sentences don't
work on slides. Normal weight, with only that bullet's key term(s) wrapped in **bold** inline —
never bold the entire bullet.

## Agenda
{{range .Agenda}}- {{.}}
{{end}}
## Chapters
{{range .Chapters}}### {{.Tag}}: {{.ModuleName}}
Overview: {{.Overview}}

{{end}}`))

type summaryPromptData struct {
	Chapters []ProtoChapterInput
	Agenda   []string
}

// buildSummaryPrompt renders the LLM prompt for the deck's closing summary bullets.
func buildSummaryPrompt(chapters []ProtoChapterInput, agenda []string) (string, error) {
	var buf bytes.Buffer
	if err := summaryPromptTmpl.Execute(&buf, summaryPromptData{Chapters: chapters, Agenda: agenda}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
