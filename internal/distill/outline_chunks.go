package distill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// outlineChunkChars is the target size of each chunk generateOutlineChunked plans slides for,
// reusing charsPerContentSlide (protodeck.go) as its calibration basis: ~outlineChunkChars/
// charsPerContentSlide = 10 slides' worth of source text per chunk — the same order of magnitude
// as expandBatchSize (6), the bullet-writing step's proven-reliable small-batch size, and the
// chunk size the user independently proposed testing ("make 10 slides for this"). Reuses
// splitIntoChunks (chunk.go), the same paragraph-respecting splitter condenseChunks already uses,
// rather than a second chunking implementation.
const outlineChunkChars = 4000

// minChunkTargetSlides and maxChunkTargetSlides are safety clamps on the per-chunk slide target
// derived from a chunk's actual character length: splitIntoChunks can occasionally emit a
// larger-than-outlineChunkChars chunk (when a single paragraph exceeds the size on its own) and a
// chapter's final chunk is often a much smaller remainder — clamping keeps the target sane at
// either extreme without needing padding or truncation elsewhere.
const (
	minChunkTargetSlides = 2
	maxChunkTargetSlides = 15
)

// generateOutlineChunked plans a proto-deck's slide topics in fixed-size chunks of each chapter's
// raw Text, instead of asking one LLM call to enumerate every topic across a whole chapter (the
// original bug: a single whole-chapter call asked to both enumerate every topic AND hit a numeric
// range anchored on the low end and silently dropped content) or asking one call per section with
// no numeric target at all (this design's immediate predecessor — see git history — which traded
// that bug for a worse one: verified against the real OpenAI API, slide count swung from 22 to
// 104 across otherwise-identical runs of the same chapter, and reconcileOutline routinely failed
// to catch resulting duplicate entries).
//
// Each chunk gets an explicit target slide count derived from its own character length (see
// generateChunkOutline) — small enough that the model reliably anchors to it (mirroring
// expandBatchSize's proven small-batch reliability) without the original whole-chapter design's
// mistake of a large, rigid range. Chunking by raw character count rather than by section is
// deliberate: a prior finding this session showed DistilledContext.Text cannot be reliably sliced
// by section (only 4 of 9 real sections cleanly substring-matched their Sections[].Title), so
// section boundaries were never a sound chunking key for this text.
//
// Chunks are processed sequentially (not in parallel), matching condenseChunks's existing
// precedent for the same rate-limit reasons (see chunk.go).
//
// The joined per-chunk results are then reconciled in one pass (reconcileOutline) to merge any
// genuine duplicates arising at chunk boundaries.
func generateOutlineChunked(ctx context.Context, llm LLM, chapters []ProtoChapterInput) (entries []outlineEntry, warnings []string, err error) {
	var joined []outlineEntry
	for i := range chapters {
		chapter := &chapters[i]
		chunks := splitIntoChunks(chapter.Text, outlineChunkChars)
		for ci, chunk := range chunks {
			target := clamp(len(chunk)/charsPerContentSlide, minChunkTargetSlides, maxChunkTargetSlides)
			chunkEntries, err := generateChunkOutline(ctx, llm, chapter, ci+1, len(chunks), chunk, target)
			if err != nil {
				return nil, nil, fmt.Errorf("chapter %q chunk %d/%d: %w", chapter.Tag, ci+1, len(chunks), err)
			}
			joined = append(joined, chunkEntries...)
		}
	}
	if len(joined) == 0 {
		return nil, nil, errors.New("outline has no slide topics across any chunk")
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

// generateChunkOutline asks llm to plan slide topics for one fixed-size chunk of a chapter's text,
// targeting approximately target slides (see generateOutlineChunked's doc comment for how target
// is derived and why a small explicit target, rather than none, is the fix for this step's
// previous count-instability problem).
func generateChunkOutline(ctx context.Context, llm LLM, chapter *ProtoChapterInput, chunkIndex, chunkTotal int, chunkText string, target int) ([]outlineEntry, error) {
	prompt, err := buildChunkOutlinePrompt(chapter, chunkIndex, chunkTotal, chunkText, target)
	if err != nil {
		return nil, fmt.Errorf("build chunk outline prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt, chunkOutlineSchema)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp chunkOutlineResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}
	entries := make([]outlineEntry, len(resp.Outline))
	for i, e := range resp.Outline {
		entries[i] = outlineEntry{Tag: chapter.Tag, Title: e.Title, Focus: e.Focus, ChunkIndices: []int{chunkIndex}}
	}
	return entries, nil
}

// chunkOutlineEntry is one entry in chunkOutlineResponse — the LLM supplies title/focus only;
// generateChunkOutline sets Tag itself from the chapter being planned, so a typo'd or
// hallucinated tag in the response can never happen.
type chunkOutlineEntry struct {
	Title string `json:"title"`
	Focus string `json:"focus"`
}

// chunkOutlineResponse is the shape of the LLM's JSON response to chunkOutlinePromptTmpl.
type chunkOutlineResponse struct {
	Outline []chunkOutlineEntry `json:"outline"`
}

// chunkOutlineSchema is chunkOutlineResponse's JSON Schema, enforced server-side for providers
// that support it (see Schema's doc comment).
var chunkOutlineSchema = &Schema{
	Name: "chunk_outline",
	Definition: jsonSchemaObject(map[string]any{
		"outline": jsonSchemaArray(jsonSchemaObject(map[string]any{
			"title": jsonSchemaString,
			"focus": jsonSchemaString,
		})),
	}),
}

// chunkOutlinePromptTmpl is the LLM prompt template for planning slide topics for one fixed-size
// chunk of a chapter's text, following condenseChunks's existing "part {{.Index}} of {{.Total}}"
// framing (chunkPromptTmpl in chunk.go) for consistency with this codebase's established
// chunking-prompt style.
var chunkOutlinePromptTmpl = template.Must(template.New("chunkoutline").Parse(`You are planning part {{.Index}} of {{.Total}} of a prototype PowerPoint outline for one textbook
chapter. Do NOT write full slide content yet — just plan which slides should exist for this
excerpt.

## Chapter
{{.Chapter.Tag}}: {{.Chapter.ModuleName}}

## This excerpt (part {{.Index}} of {{.Total}} of the chapter's text, in order)
{{.ChunkText}}

Ignore any "Participation Activity", "Animation content/captions", "Static figure" caption, or
"Check"/"Show answer" text in the excerpt above — these are leftover interactive-widget artifacts,
not real content, and must not become outline entries. A "Worked Example"/"WE#" with a complete
solution is real content and should get its own entry.

Do NOT plan a slide for any "Lab", "Python Lab", or other hands-on coding-exercise section — labs
are a separate hands-on assignment, not lecture content, even though they're real material in the
excerpt.

This excerpt is being planned independently from every other part of the chapter — any overlap
with another part's material will be resolved in a separate pass afterward, so do not skip or
merge a distinct topic just because it might also relate to another part.

## Task
Plan approximately {{.Target}} slides for this excerpt: enumerate its most important distinct
definitions, formulas, rules, and worked examples, one outline entry per topic, never combining
several ideas into one entry. Treat {{.Target}} as a strong guide for how granular to be, not an
exact quota — a couple more or fewer is fine if this excerpt's actual distinct content calls for
it, but do not pad with filler or artificially merge unrelated topics just to hit the number.

Produce a JSON object: {"outline": [{"title": "...", "focus": "..."}]}, where title is a SHORT
slide heading (2-6 words, a noun phrase like "Zero Vector" or "Worked Example: Vector Magnitude" —
never a full sentence and never punctuated with a dash/colon followed by an explanation) and focus
is a separate 1-2 sentence description of exactly what that slide should cover (referencing a
specific definition, formula, or worked example) — NOT the slide's actual bullet content, just
what it should be about. Any explanation of what the slide covers belongs in focus, never appended
to title.
`))

type chunkOutlinePromptData struct {
	Chapter   *ProtoChapterInput
	Index     int
	Total     int
	ChunkText string
	Target    int
}

// buildChunkOutlinePrompt renders the LLM prompt for planning one chunk's slide topics.
func buildChunkOutlinePrompt(chapter *ProtoChapterInput, index, total int, chunkText string, target int) (string, error) {
	var buf bytes.Buffer
	data := chunkOutlinePromptData{Chapter: chapter, Index: index, Total: total, ChunkText: chunkText, Target: target}
	if err := chunkOutlinePromptTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// reconcileOutline merges genuine duplicate topics out of entries — the risk introduced by
// planning chunks independently (generateChunkOutline) rather than in one call that could see
// every other chunk's plan. Follows the same contract as this package's verifyConsistency
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
		// The joined outline is rendered to the model as "N. [tag] (chunk N) Title — Focus" (see
		// reconcileOutlinePromptTmpl); observed in practice against the real API: the model
		// sometimes echoes the tag back including the surrounding brackets ("[ch01]") instead of
		// just the tag itself ("ch01"), and separately, sometimes folds the chunk annotation into
		// the tag string too ("ch01#2") despite tag and chunk_indices being distinct schema
		// fields. Strip both defensively rather than trust the model to reproduce the
		// delimiter-free value every time — every entry silently failing this check (because none
		// matched knownTags) is exactly what caused generateOutlineChunked's "no slide topics
		// after reconciliation" error to fire on every real-API run before this fix.
		tag := strings.Trim(strings.TrimSpace(e.Tag), "[]")
		if i := strings.IndexByte(tag, '#'); i >= 0 {
			tag = tag[:i]
		}
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
			"tag":           jsonSchemaString,
			"title":         jsonSchemaString,
			"focus":         jsonSchemaString,
			"chunk_indices": jsonSchemaArray(jsonSchemaInteger),
		})),
		"warnings": jsonSchemaArray(jsonSchemaString),
	}),
}

// reconcileOutlinePromptTmpl is the LLM prompt template for merging duplicate topics out of a
// joined, independently-planned-per-chunk outline.
//
// Each entry is rendered as "N. [tag] (chunk C) Title — Focus", one per line — the "[tag]" part
// keeps the exact "N. [tag] Title — Focus" shape buildExpandBatchPrompt already uses for its
// planned-slides list (see outline.go), reused here for consistency and because it's a stable,
// easily-parsed format; the chunk annotation is a visually separate parenthetical, deliberately
// NOT crammed inside the brackets with tag (an earlier "[tag#chunkIndex]" combined format was
// tried and, observed in practice against the real OpenAI API, led the model to echo the whole
// combined string back as the tag value instead of splitting it into the two distinct schema
// fields tag/chunk_indices actually are).
var reconcileOutlinePromptTmpl = template.Must(template.New("reconcileoutline").Parse(`You are reviewing a slide outline that was planned in fixed-size chunks of chapter text, one
chunk at a time, independently, which can produce duplicate entries where two chunks' material
overlaps at a boundary.

## Planned outline
{{range $i, $e := .Entries}}{{$i}}. [{{$e.Tag}}] (chunk {{index $e.ChunkIndices 0}}) {{$e.Title}} — {{$e.Focus}}
{{end}}
## Task
Produce a JSON object: {"outline": [{"tag": "...", "title": "...", "focus": "...", "chunk_indices": [...]}], "warnings": ["..."]}.

- outline: the list above, with duplicate entries merged. EVERY object must include all four
  fields (tag, title, focus, chunk_indices) as four SEPARATE fields — never omit tag or leave it
  blank, and never combine tag with the chunk number into one string. tag/title/focus are copied
  verbatim from the kept (or more complete) source entry — tag is ONLY the bracketed value (e.g.
  "ch01"), never the "(chunk N)" annotation next to it. chunk_indices is that source entry's chunk
  number (the N in "(chunk N)") as a one-element array when keeping an entry as-is, or the union
  of both entries' chunk numbers as a two-element array when merging (e.g. merging an entry from
  chunk 2 with one from chunk 3 produces chunk_indices: [2, 3]). Only merge two entries if they
  describe the EXACT SAME specific content restated differently — the same formula, the same
  definition, the same worked example. Do NOT merge entries that are merely related or in the same
  topic area ("Matrix Addition" and "Matrix Subtraction" are DIFFERENT entries and must both be
  kept). Preserve the original relative order of the entries you keep.
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
// generateOutlineChunked/generateChunkOutline's doc comments for why topic enumeration is
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
