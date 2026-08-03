package distill

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

// charsPerContentSlide is the calibration constant behind AutoSlideRange: the number of
// condensed-text characters that comfortably fill one content slide's worth of bullets.
// Calibrated against a real distilled chapter (a 9-section, ~94-page zyBooks chapter condensing
// to ~14.2K chars of Text) where a 1-hour-lecture-worthy deck needs roughly 35-45 content slides
// — about 350-400 chars/slide — so 400 was picked to land the target near the lower end of a
// human-reviewed-good range rather than over-splitting sparse chapters.
const charsPerContentSlide = 400

// minContentSlides and maxContentSlides bound AutoSlideRange's content-slide estimate regardless
// of chapter length: minContentSlides keeps very short/thin sources from producing a
// single-slide-per-topic deck that's too sparse to present from, and maxContentSlides caps how
// far a single deck can grow before it should probably be split into multiple decks instead.
const (
	minContentSlides = 6
	maxContentSlides = 120
)

// AutoSlideRange estimates a (minSlides, maxSlides) range for GenerateProtoDeck sized to the
// combined condensed Text length of chapters, rather than a flat default — a short chapter and a
// dense multi-section chapter both fed a fixed 8-30 range would either force padding on the short
// one or force under-coverage on the long one (the failure this fixes: a 6-section, ~94-page
// chapter landing on only 17 content slides because the deck's own length told the outline model
// nothing about how much ground it needed to cover).
//
// The range spans -15%/+25% around the point estimate rather than returning a single number,
// since generateOutline plans better against a range it can exercise judgment within than a
// single exact target (see GenerateProtoDeck's doc comment on why outline generation is a
// bounded-enumeration task, not a precise one).
func AutoSlideRange(chapters []ProtoChapterInput) (minSlides, maxSlides int) {
	var totalChars int
	for _, ch := range chapters {
		totalChars += len(ch.Text)
	}

	target := totalChars / charsPerContentSlide
	minContent := clamp(target*85/100, minContentSlides, maxContentSlides)
	maxContent := clamp(target*125/100, minContentSlides, maxContentSlides)
	if maxContent < minContent {
		maxContent = minContent
	}

	// +2 for the deck's fixed agenda and summary slides, which minSlides/maxSlides count
	// alongside content slides (see GenerateProtoDeck).
	return minContent + 2, maxContent + 2
}

func clamp(n, lo, hi int) int {
	switch {
	case n < lo:
		return lo
	case n > hi:
		return hi
	default:
		return n
	}
}

var reProtoMeta = regexp.MustCompile(`(?m)^<!-- meta:\s*(\d+)\s+(\S+)\s*-->\s*$`)

// GenerateProtoDeck asks llm to produce a single markdown proto-slide deck spanning all of
// chapters (in order), targeting between minSlides and maxSlides total numbered slides (an
// agenda slide, one or more content slides per chapter, and a closing summary slide). The
// returned markdown is validated for the meta-marker slide count and numbering before it's
// returned to the caller.
//
// Generation is two-pass rather than one-shot: a single call asked to both plan AND write the
// full content of a 30+ slide deck reliably undershoots the requested count — the same failure
// mode DistilledContext.Text hit before its fix (models don't self-regulate a large numeric
// target well while also generating prose, no matter how the instruction is worded). Instead,
// generateOutline asks for a flat list of slide topics (a bounded enumeration task models hit
// close to target on), and expandOutline writes each topic's bullets in small batches, grounded
// in the chapter's condensed text.
func GenerateProtoDeck(ctx context.Context, llm LLM, chapters []ProtoChapterInput, minSlides, maxSlides int) (string, error) {
	if len(chapters) == 0 {
		return "", errors.New("no chapters to build a proto deck from")
	}
	if minSlides <= 0 || maxSlides <= 0 || minSlides > maxSlides {
		return "", fmt.Errorf("invalid slide range %d-%d", minSlides, maxSlides)
	}

	// minSlides/maxSlides count agenda + content + summary; the outline's budget is just the
	// content slides, excluding those two fixed slides.
	minContent := minSlides - 2
	if minContent < 1 {
		minContent = 1
	}
	maxContent := maxSlides - 2
	if maxContent < minContent {
		maxContent = minContent
	}

	outline, err := generateOutline(ctx, llm, chapters, minContent, maxContent)
	if err != nil {
		return "", fmt.Errorf("generate outline: %w", err)
	}

	deck, err := expandOutline(ctx, llm, chapters, outline)
	if err != nil {
		return "", fmt.Errorf("expand outline: %w", err)
	}

	if err := validateProtoDeck(deck, minSlides, maxSlides); err != nil {
		return "", fmt.Errorf("invalid proto deck: %w", err)
	}
	return deck, nil
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
