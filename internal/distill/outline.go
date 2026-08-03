package distill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// outlineEntry is one planned content slide: which chapter it belongs to, its title, and a short
// description of what it should cover — grounded enough for expandBatch to write real bullets
// from, without needing each chapter's full text repeated in the outline call itself.
type outlineEntry struct {
	Tag   string `json:"tag"`
	Title string `json:"title"`
	Focus string `json:"focus"`
}

// outlineResponse is the shape of the LLM's JSON response to outlinePromptTmpl.
type outlineResponse struct {
	DeckTitle string         `json:"deck_title"`
	Agenda    []string       `json:"agenda"`
	Outline   []outlineEntry `json:"outline"`
}

// generateOutline asks llm to plan a proto-deck as a flat list of slide topics — a bounded
// enumeration task LLMs reliably hit close to a numeric target on — rather than writing the full
// content of a large deck in one shot, which they don't (see GenerateProtoDeck's doc comment).
func generateOutline(ctx context.Context, llm LLM, chapters []ProtoChapterInput, minContent, maxContent int) (*outlineResponse, error) {
	prompt, err := buildOutlinePrompt(chapters, minContent, maxContent)
	if err != nil {
		return nil, fmt.Errorf("build outline prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp outlineResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	if len(resp.Outline) > maxContent {
		resp.Outline = resp.Outline[:maxContent]
	}
	if len(resp.Outline) < minContent {
		return nil, fmt.Errorf("outline has %d slide topics, need at least %d", len(resp.Outline), minContent)
	}
	if n := len(resp.Agenda); n < 3 || n > 8 {
		return nil, fmt.Errorf("outline agenda has %d bullets, need 3-8", n)
	}
	return &resp, nil
}

// outlinePromptTmpl is the LLM prompt template for planning a proto-deck's slide topics, ahead
// of writing their actual content.
var outlinePromptTmpl = template.Must(template.New("outline").Parse(`You are planning a prototype PowerPoint outline spanning multiple textbook chapters. Do NOT
write full slide content yet — just plan which slides should exist.

## Chapters
{{range .Chapters}}### {{.Tag}}: {{.ModuleName}}
Overview: {{.Overview}}
Key concepts: {{range .KeyConcepts}}{{.}}, {{end}}
Teaching notes: {{.TeachingNotes}}

Full chapter text (use this to find every distinct definition, formula, and worked example to
plan a slide for — the fields above are only a high-level summary and omit most of this detail):
{{.Text}}

{{end}}
Ignore any "Participation Activity", "Animation content/captions", "Static figure" caption, or
"Check"/"Show answer" text in the chapter text above — these are leftover interactive-widget
artifacts, not real content, and must not become outline entries. A "Worked Example"/"WE#" with a
complete solution is real content and should get its own entry.

Do NOT plan a slide for any "Lab", "Python Lab", or other hands-on coding-exercise section — labs
are a separate hands-on assignment, not lecture content, even though they're real material in the
chapter text.

Every outline entry must cover NEW material not covered by any other entry — a specific
definition, formula, rule, or worked example. Do NOT plan a slide whose purpose is to recap,
review, or highlight material already covered elsewhere in the outline, under ANY title
("Review", "Summary", "Recap", "Key Points", "Key Takeaways", "What We Learned", etc.) — a closing
summary slide covering the whole deck is always appended automatically after your last outline
entry, so any such slide in the outline itself would be redundant with it.

## Task
Produce a JSON object with these exact fields:
- deck_title: short overall title for the deck
- agenda: array of 3-8 short noun-phrase strings (2-6 words each), one per major cross-chapter
  theme, each matching the title of the content slide it corresponds to — not a full sentence
- outline: array of {tag, title, focus} objects, one per planned content slide, in chapter order,
  where tag is one of the chapter tags above and focus is a 1-2 sentence description of exactly
  what that slide should cover (referencing a specific definition, formula, or worked example) —
  NOT the slide's actual bullet content, just what it should be about

## Slide count
Produce between {{.MinContent}} and {{.MaxContent}} outline entries total. Reach this by planning
ONE topic per slide, never combining several ideas into one entry: a chapter with several
subsections and worked examples should easily need {{.MinContent}}+ entries once you plan one
slide per definition/concept, one per formula or rule, and one per worked example (never grouped).

TARGET_CONTENT_RANGE: min={{.MinContent}} max={{.MaxContent}}
`))

type outlinePromptData struct {
	Chapters   []ProtoChapterInput
	MinContent int
	MaxContent int
}

// buildOutlinePrompt renders the LLM prompt for planning a proto-deck's slide topics.
func buildOutlinePrompt(chapters []ProtoChapterInput, minContent, maxContent int) (string, error) {
	var buf bytes.Buffer
	if err := outlinePromptTmpl.Execute(&buf, outlinePromptData{Chapters: chapters, MinContent: minContent, MaxContent: maxContent}); err != nil {
		return "", err
	}
	return buf.String(), nil
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
	for _, c := range chapters {
		chapterByTag[c.Tag] = c
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

// expandBatch writes bullet content for batch's outline entries in one LLM call, grounded in the
// source text of every chapter tag referenced in batch.
func expandBatch(ctx context.Context, llm LLM, chapterByTag map[string]ProtoChapterInput, batch []outlineEntry) ([][]string, error) {
	prompt, err := buildExpandBatchPrompt(chapterByTag, batch)
	if err != nil {
		return nil, fmt.Errorf("build expand prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp expandBatchResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	out := make([][]string, len(resp.Slides))
	for i, s := range resp.Slides {
		out[i] = s.Bullets
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
`))

type expandBatchPromptData struct {
	Chapters []ProtoChapterInput
	Batch    []outlineEntry
}

// buildExpandBatchPrompt renders the LLM prompt for writing a batch of planned slides' content,
// including only the chapters actually referenced by batch (not every chapter in the module).
func buildExpandBatchPrompt(chapterByTag map[string]ProtoChapterInput, batch []outlineEntry) (string, error) {
	seen := make(map[string]bool, len(batch))
	var chapters []ProtoChapterInput
	for _, e := range batch {
		if seen[e.Tag] {
			continue
		}
		seen[e.Tag] = true
		if c, ok := chapterByTag[e.Tag]; ok {
			chapters = append(chapters, c)
		}
	}
	var buf bytes.Buffer
	if err := expandBatchPromptTmpl.Execute(&buf, expandBatchPromptData{Chapters: chapters, Batch: batch}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// expandSummary writes the deck's closing summary bullets: one fuller recap per agenda item,
// rather than one generic takeaway per chapter — for a single-chapter deck, "one per chapter"
// collapses to just one or two bullets total, far too thin for a real closing slide.
func expandSummary(ctx context.Context, llm LLM, chapters []ProtoChapterInput, agenda []string) ([]string, error) {
	prompt, err := buildSummaryPrompt(chapters, agenda)
	if err != nil {
		return nil, fmt.Errorf("build summary prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt)
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
	return resp.Bullets, nil
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
