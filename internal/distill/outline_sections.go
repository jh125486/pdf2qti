package distill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// generateOutlineChunked plans a proto-deck's slide topics one textbook section at a time,
// instead of asking one LLM call to enumerate every topic across a whole chapter — see this
// package's outline_sections_doc note in generateSectionOutline for why. Sections are visited in
// order, chapter by chapter, and each per-section call is made sequentially (not in parallel),
// matching condenseChunks's existing precedent for the same rate-limit reasons (see chunk.go).
//
// A chapter with no Sections gets one synthesized pseudo-section from its own ModuleName/Overview
// — the same generation path handles both cases, rather than forking a separate whole-chapter
// fallback implementation.
//
// The joined per-section results are then reconciled in one pass (reconcileOutline) to merge any
// genuine cross-section or cross-chapter duplicates.
func generateOutlineChunked(ctx context.Context, llm LLM, chapters []ProtoChapterInput) (entries []outlineEntry, warnings []string, err error) {
	var joined []outlineEntry
	for i := range chapters {
		chapter := &chapters[i]
		sections := chapter.Sections
		if len(sections) == 0 {
			sections = []Section{{Title: chapter.ModuleName, Summary: chapter.Overview}}
		}
		for _, section := range sections {
			sectionEntries, err := generateSectionOutline(ctx, llm, chapter, section)
			if err != nil {
				return nil, nil, fmt.Errorf("chapter %q section %q: %w", chapter.Tag, section.Title, err)
			}
			joined = append(joined, sectionEntries...)
		}
	}
	if len(joined) == 0 {
		return nil, nil, errors.New("outline has no slide topics across any section")
	}

	knownTags := make(map[string]bool, len(chapters))
	for i := range chapters {
		knownTags[chapters[i].Tag] = true
	}

	reconciled, warnings, err := reconcileOutline(ctx, llm, joined, knownTags)
	if err != nil {
		return nil, nil, fmt.Errorf("reconcile outline: %w", err)
	}
	if len(reconciled) == 0 {
		return nil, nil, errors.New("outline has no slide topics after reconciliation")
	}
	return reconciled, warnings, nil
}

// generateSectionOutline asks llm to plan slide topics for exactly one textbook section, scoped
// by the section's own Title/Summary, with the chapter's full Text as grounding for exact
// formulas/wording/worked examples. Deliberately carries no numeric slide-count target: the
// previous whole-chapter design asked one call to both enumerate every topic across an entire
// chapter AND hit a numeric range, and empirically (verified against the real OpenAI API) that
// combination made the model anchor on the low end of the range and silently drop whole topics
// to get there — two rounds of prompt-wording fixes for that couldn't reliably fix it. Scoping
// each call to one section's worth of material removes the need for a target at all: enumerating
// every distinct topic in one section is a small enough task that the model doesn't need
// numeric guidance to do it thoroughly.
func generateSectionOutline(ctx context.Context, llm LLM, chapter *ProtoChapterInput, section Section) ([]outlineEntry, error) {
	prompt, err := buildSectionOutlinePrompt(chapter, section)
	if err != nil {
		return nil, fmt.Errorf("build section outline prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, sectionOutlineSchema)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp sectionOutlineResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	entries := make([]outlineEntry, len(resp.Outline))
	for i, e := range resp.Outline {
		entries[i] = outlineEntry{Tag: chapter.Tag, Title: e.Title, Focus: e.Focus}
	}
	return entries, nil
}

// sectionOutlineEntry is one entry in sectionOutlineResponse — the LLM supplies title/focus only;
// generateSectionOutline sets Tag itself from the chapter being planned, so a typo'd or
// hallucinated tag in the response can never happen.
type sectionOutlineEntry struct {
	Title string `json:"title"`
	Focus string `json:"focus"`
}

// sectionOutlineResponse is the shape of the LLM's JSON response to sectionOutlinePromptTmpl.
type sectionOutlineResponse struct {
	Outline []sectionOutlineEntry `json:"outline"`
}

// sectionOutlineSchema is sectionOutlineResponse's JSON Schema, enforced server-side for
// providers that support it (see Schema's doc comment).
var sectionOutlineSchema = &Schema{
	Name: "section_outline",
	Definition: jsonSchemaObject(map[string]any{
		"outline": jsonSchemaArray(jsonSchemaObject(map[string]any{
			"title": jsonSchemaString,
			"focus": jsonSchemaString,
		})),
	}),
}

// sectionOutlinePromptTmpl is the LLM prompt template for planning slide topics for one textbook
// section at a time.
var sectionOutlinePromptTmpl = template.Must(template.New("sectionoutline").Parse(`You are planning a small part of a prototype PowerPoint outline: just the slides for ONE section
of one textbook chapter. Do NOT write full slide content yet — just plan which slides should
exist for this section.

## Chapter
{{.Chapter.Tag}}: {{.Chapter.ModuleName}}

## This section
Title: {{.Section.Title}}
Summary: {{.Section.Summary}}

## Full chapter text (grounding only — use this to find this section's exact definitions,
formulas, and worked examples; ignore any material here that belongs to a DIFFERENT section)
{{.Chapter.Text}}

Ignore any "Participation Activity", "Animation content/captions", "Static figure" caption, or
"Check"/"Show answer" text in the chapter text above — these are leftover interactive-widget
artifacts, not real content, and must not become outline entries. A "Worked Example"/"WE#" with a
complete solution is real content and should get its own entry.

Do NOT plan a slide for any "Lab", "Python Lab", or other hands-on coding-exercise section — labs
are a separate hands-on assignment, not lecture content, even though they're real material in the
chapter text.

This section is being planned independently from every other section in the chapter — any overlap
with another section's material will be resolved in a separate pass afterward, so do not skip or
merge a distinct topic just because it might also relate to another section.

## Task
Enumerate every distinct definition, formula, rule, and worked example that is part of THIS
section specifically — one outline entry per topic, never combining several ideas into one entry.
A section with several definitions and a worked example needs that many entries, not one summary
entry for the whole section. There is no target count to hit or avoid: plan exactly as many
entries as this section's actual distinct content requires, however many or few that is.

Produce a JSON object: {"outline": [{"title": "...", "focus": "..."}]}, where focus is a 1-2
sentence description of exactly what that slide should cover (referencing a specific definition,
formula, or worked example) — NOT the slide's actual bullet content, just what it should be about.
`))

type sectionOutlinePromptData struct {
	Chapter *ProtoChapterInput
	Section Section
}

// buildSectionOutlinePrompt renders the LLM prompt for planning one section's slide topics.
func buildSectionOutlinePrompt(chapter *ProtoChapterInput, section Section) (string, error) {
	var buf bytes.Buffer
	if err := sectionOutlinePromptTmpl.Execute(&buf, sectionOutlinePromptData{Chapter: chapter, Section: section}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// reconcileOutline merges genuine duplicate topics out of entries — the risk introduced by
// planning sections independently (generateSectionOutline) rather than in one call that could see
// every other section's plan. Follows the same contract as this package's verifyConsistency
// (verify.go): the LLM returns a full replacement list rather than a diff, Go overwrites rather
// than merges, and anything the LLM couldn't confidently resolve comes back as a warning for
// human review instead of being silently dropped or silently kept as a duplicate.
//
// Works from entries' tag/title/focus only, not chapters' full Text — merge decisions ("do these
// two entries describe the same specific formula, restated") are answerable from short grounded
// descriptions alone, and keeping this call's size independent of chapter/module length matters
// for a multi-chapter module, where the old whole-chapter outline call's prompt scaled with total
// chapter text and this one otherwise would too.
func reconcileOutline(ctx context.Context, llm LLM, entries []outlineEntry, knownTags map[string]bool) (reconciled []outlineEntry, warnings []string, err error) {
	prompt, err := buildReconcileOutlinePrompt(entries)
	if err != nil {
		return nil, nil, fmt.Errorf("build reconcile prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, reconcileOutlineSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp reconcileOutlineResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, nil, fmt.Errorf("parse llm response: %w", err)
	}

	warnings = resp.Warnings
	reconciled = make([]outlineEntry, 0, len(resp.Outline))
	for _, e := range resp.Outline {
		// The joined outline is rendered to the model as "N. [tag] Title — Focus" (see
		// reconcileOutlinePromptTmpl); observed in practice against the real API: the model
		// sometimes echoes the tag back including the surrounding brackets ("[ch01]") instead of
		// just the tag itself ("ch01"). Strip them defensively rather than trust the model to
		// reproduce the delimiter-free value every time — every entry silently failing this
		// check (because none matched knownTags) is exactly what caused
		// generateOutlineChunked's "no slide topics after reconciliation" error to fire on every
		// real-API run before this fix.
		tag := strings.Trim(strings.TrimSpace(e.Tag), "[]")
		if !knownTags[tag] {
			warnings = append(warnings, fmt.Sprintf("reconciled outline entry %q has unknown tag %q, dropped", e.Title, e.Tag))
			continue
		}
		e.Tag = tag
		reconciled = append(reconciled, e)
	}
	return reconciled, warnings, nil
}

// reconcileOutlineResponse is the shape of the LLM's JSON response to reconcileOutlinePromptTmpl.
type reconcileOutlineResponse struct {
	Outline  []outlineEntry `json:"outline"`
	Warnings []string       `json:"warnings"`
}

// reconcileOutlineSchema is reconcileOutlineResponse's JSON Schema, enforced server-side for
// providers that support it (see Schema's doc comment). This is the call site whose
// prose-described response shape (before this schema was added) let the model omit or mangle the
// "tag" field in practice against the real OpenAI API — see reconcileOutline's defensive
// bracket-stripping, kept as a second line of defense even with this schema enforced.
var reconcileOutlineSchema = &Schema{
	Name: "reconciled_outline",
	Definition: jsonSchemaObject(map[string]any{
		"outline": jsonSchemaArray(jsonSchemaObject(map[string]any{
			"tag":   jsonSchemaString,
			"title": jsonSchemaString,
			"focus": jsonSchemaString,
		})),
		"warnings": jsonSchemaArray(jsonSchemaString),
	}),
}

// reconcileOutlinePromptTmpl is the LLM prompt template for merging duplicate topics out of a
// joined, independently-planned-per-section outline.
//
// Each entry is rendered as "N. [tag] Title — Focus", one per line — the same "N. [tag] Title —
// Focus" shape buildExpandBatchPrompt already uses for its planned-slides list (see outline.go),
// reused here for consistency and because it's a stable, easily-parsed format.
var reconcileOutlinePromptTmpl = template.Must(template.New("reconcileoutline").Parse(`You are reviewing a slide outline that was planned one textbook section at a time, independently,
which can produce duplicate entries where two sections' material overlaps at a boundary.

## Planned outline
{{range $i, $e := .Entries}}{{$i}}. [{{$e.Tag}}] {{$e.Title}} — {{$e.Focus}}
{{end}}
## Task
Produce a JSON object: {"outline": [{"tag": "...", "title": "...", "focus": "..."}], "warnings": ["..."]}.

- outline: the list above, with duplicate entries merged. EVERY object must include all three
  fields (tag, title, focus) copied verbatim from its source entry above — never omit tag or
  leave it blank, even when merging two entries into one. Only merge two entries if they describe
  the EXACT SAME specific content restated differently — the same formula, the same definition,
  the same worked example. Do NOT merge entries that are merely related or in the same topic area
  ("Matrix Addition" and "Matrix Subtraction" are DIFFERENT entries and must both be kept). When
  merging, keep the tag of either entry and the more complete/specific focus of the two. Preserve
  the original relative order of the entries you keep.
- warnings: array of short strings, one per case you weren't confident enough to resolve on your
  own (e.g. two entries that might be the same topic but you're not sure); empty array if none
`))

type reconcileOutlinePromptData struct {
	Entries []outlineEntry
}

// buildReconcileOutlinePrompt renders the LLM prompt for reconciling a joined per-section outline.
func buildReconcileOutlinePrompt(entries []outlineEntry) (string, error) {
	var buf bytes.Buffer
	if err := reconcileOutlinePromptTmpl.Execute(&buf, reconcileOutlinePromptData{Entries: entries}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// generateAgenda asks llm to frame the deck: a short overall title, and a 3-8 item agenda
// summarizing the reconciled outline's major cross-chapter themes. Kept as its own call, after
// outline planning is fully reconciled, rather than bundled into the same call that plans slide
// topics — deck framing and topic enumeration are different task shapes (see
// generateOutlineChunked/generateSectionOutline's doc comments for why topic enumeration is
// itself now split out from a single whole-chapter call).
func generateAgenda(ctx context.Context, llm LLM, chapters []ProtoChapterInput, entries []outlineEntry) (*agendaResponse, error) {
	prompt, err := buildAgendaPrompt(chapters, entries)
	if err != nil {
		return nil, fmt.Errorf("build agenda prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, agendaSchema)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp agendaResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	if n := len(resp.Agenda); n < 3 || n > 8 {
		return nil, fmt.Errorf("agenda has %d bullets, need 3-8", n)
	}
	return &resp, nil
}

// agendaResponse is the shape of the LLM's JSON response to agendaPromptTmpl.
type agendaResponse struct {
	DeckTitle string   `json:"deck_title"`
	Agenda    []string `json:"agenda"`
}

// agendaSchema is agendaResponse's JSON Schema, enforced server-side for providers that support
// it (see Schema's doc comment).
var agendaSchema = &Schema{
	Name: "deck_agenda",
	Definition: jsonSchemaObject(map[string]any{
		"deck_title": jsonSchemaString,
		"agenda":     jsonSchemaArray(jsonSchemaString),
	}),
}

// agendaPromptTmpl is the LLM prompt template for framing a deck's overall title and agenda from
// its already-reconciled, fully-planned outline.
var agendaPromptTmpl = template.Must(template.New("agenda").Parse(`You are writing the overall title and agenda for a prototype PowerPoint deck whose slide topics
have already been fully planned below — do NOT add, remove, or change any planned topic.

## Chapters
{{range .Chapters}}### {{.Tag}}: {{.ModuleName}}
Overview: {{.Overview}}

{{end}}
## Planned slide topics
{{range .Entries}}- [{{.Tag}}] {{.Title}}
{{end}}
## Task
Produce a JSON object: {"deck_title": "...", "agenda": ["...", "..."]}.

- deck_title: short overall title for the deck
- agenda: array of 3-8 short noun-phrase strings (2-6 words each), one per major cross-chapter
  theme, each matching the title of the planned slide it corresponds to — not a full sentence
`))

type agendaPromptData struct {
	Chapters []ProtoChapterInput
	Entries  []outlineEntry
}

// buildAgendaPrompt renders the LLM prompt for framing the deck's title and agenda.
func buildAgendaPrompt(chapters []ProtoChapterInput, entries []outlineEntry) (string, error) {
	var buf bytes.Buffer
	if err := agendaPromptTmpl.Execute(&buf, agendaPromptData{Chapters: chapters, Entries: entries}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
