package distill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// ProtoChapterInput is one chapter's distilled material used as source for a multi-chapter
// proto-slide deck. Tag identifies the chapter in the deck's meta markers and should be a
// stable identifier for the chapter (e.g. its config.Source.ID), since downstream PPTX
// generation groups slides into sections by matching this tag.
type ProtoChapterInput struct {
	Tag           string
	ModuleName    string
	Overview      string
	KeyConcepts   []string
	TeachingNotes string
	Text          string
}

var reProtoMeta = regexp.MustCompile(`(?m)^<!-- meta:\s*(\d+)\s+(\S+)\s*-->\s*$`)

// GenerateProtoDeck asks llm to produce a single markdown proto-slide deck spanning all of
// chapters (in order), targeting between minSlides and maxSlides total numbered slides (an
// agenda slide, one or more content slides per chapter, and a closing summary slide). The
// returned markdown is validated for the meta-marker slide count and numbering before it's
// returned to the caller.
func GenerateProtoDeck(ctx context.Context, llm LLM, chapters []ProtoChapterInput, minSlides, maxSlides int) (string, error) {
	if len(chapters) == 0 {
		return "", errors.New("no chapters to build a proto deck from")
	}
	if minSlides <= 0 || maxSlides <= 0 || minSlides > maxSlides {
		return "", fmt.Errorf("invalid slide range %d-%d", minSlides, maxSlides)
	}

	prompt, err := buildProtoDeckPrompt(chapters, minSlides, maxSlides)
	if err != nil {
		return "", fmt.Errorf("build proto deck prompt: %w", err)
	}

	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("llm complete: %w", err)
	}

	if err := validateProtoDeck(raw, minSlides, maxSlides); err != nil {
		return "", fmt.Errorf("invalid proto deck: %w", err)
	}
	return raw, nil
}

// validateProtoDeck checks that deck has a slide count within [minSlides, maxSlides] and that
// its <!-- meta: N tag --> markers are sequential starting at 1, with no gaps or repeats.
func validateProtoDeck(deck string, minSlides, maxSlides int) error {
	matches := reProtoMeta.FindAllStringSubmatch(deck, -1)
	if got := len(matches); got < minSlides || got > maxSlides {
		return fmt.Errorf("expected between %d and %d slides, got %d", minSlides, maxSlides, got)
	}
	for i, m := range matches {
		want := i + 1
		got, err := strconv.Atoi(m[1])
		if err != nil || got != want {
			return fmt.Errorf("slide meta numbers must be sequential starting at 1: expected %d, got %q", want, m[1])
		}
	}
	return nil
}

// protoDeckPromptTmpl is the LLM prompt template for multi-chapter proto-slide deck generation.
var protoDeckPromptTmpl = template.Must(template.New("protodeck").Parse(`You are building a prototype PowerPoint outline in Markdown, spanning multiple textbook chapters.

## Chapters
{{range .Chapters}}### {{.Tag}}: {{.ModuleName}}
Overview: {{.Overview}}
Key concepts: {{range .KeyConcepts}}{{.}}, {{end}}
Teaching notes: {{.TeachingNotes}}

{{end}}
## Task
Produce ONE markdown proto-slide deck covering all chapters above, structured exactly like this:

    # <overall deck title>

    ---

    <!-- meta: 1 agenda -->
    # Agenda

    - <3-8 bullets, one per major cross-chapter theme>

    ---

    <!-- meta: 2 {{(index .Chapters 0).Tag}} -->
    # <slide title>

    - <5-8 bullets covering this chapter's key points>

    ---

    ... one slide per notable subsection of each chapter, in chapter order, tagged with that
    chapter's tag from the list above ...

    <!-- meta: N summary -->
    # Summary

    - <one bolded takeaway bullet per chapter>

## Rules
- Every slide except the very first (untagged) title slide is preceded by a
  "<!-- meta: N tag -->" comment, where N is a 1-based sequential slide number with no gaps and
  tag is "agenda", "summary", or one of the chapter tags above.
- Slides are separated by a line containing only "---".
- The deck must have between {{.MinSlides}} and {{.MaxSlides}} numbered slides total (agenda +
  content + summary combined). Allocate more content slides to chapters with more material.

TARGET_SLIDE_RANGE: min={{.MinSlides}} max={{.MaxSlides}}
`))

// protoDeckPromptData is the data passed to protoDeckPromptTmpl.
type protoDeckPromptData struct {
	Chapters  []ProtoChapterInput
	MinSlides int
	MaxSlides int
}

// buildProtoDeckPrompt renders the LLM prompt for multi-chapter proto-slide deck generation.
func buildProtoDeckPrompt(chapters []ProtoChapterInput, minSlides, maxSlides int) (string, error) {
	var buf bytes.Buffer
	if err := protoDeckPromptTmpl.Execute(&buf, protoDeckPromptData{
		Chapters:  chapters,
		MinSlides: minSlides,
		MaxSlides: maxSlides,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// reProtoSeparator matches a "---" slide-separator line, the format GenerateProtoDeck emits and
// ParseProtoDeck reads back.
var reProtoSeparator = regexp.MustCompile(`(?m)^---\s*$`)

// ParseProtoDeck is the inverse of GenerateProtoDeck: it parses proto-deck markdown — whether
// produced by GenerateProtoDeck or written by hand in the same format — into the pieces
// pptx.Render needs: the overall deck title, the agenda bullets, and one Slide per non-agenda
// block in document order ("summary" is included as a regular slide since there's no dedicated
// Summary layout in the PPTX template contract).
func ParseProtoDeck(markdown string) (title string, agenda []string, slides []Slide, err error) {
	blocks := reProtoSeparator.Split(markdown, -1)
	if len(blocks) == 0 {
		return "", nil, nil, errors.New("empty proto deck markdown")
	}

	title = firstHeading(blocks[0])

	for _, block := range blocks[1:] {
		m := reProtoMeta.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		bullets := bulletLines(block)
		tag := m[2]
		if tag == "agenda" {
			agenda = bullets
			continue
		}
		slides = append(slides, Slide{Title: firstHeading(block), Content: strings.Join(bullets, "\n"), Tag: tag})
	}

	if len(agenda) == 0 && len(slides) == 0 {
		return "", nil, nil, errors.New("no agenda or slides found in proto deck markdown")
	}
	return title, agenda, slides, nil
}

// firstHeading returns the text of the first "# " line in block, or "" if none.
func firstHeading(block string) string {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// bulletLines returns the text of every "- " line in block, in order, with the marker stripped.
func bulletLines(block string) []string {
	var bullets []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "- "); ok {
			bullets = append(bullets, strings.TrimSpace(after))
		}
	}
	return bullets
}
