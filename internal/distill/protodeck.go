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
	Sections      []Section
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

// AutoSlideRange estimates a (minSlides, maxSlides) range sized to the combined condensed Text
// length of chapters, rather than a flat default — a short chapter and a dense multi-section
// chapter both fed a fixed 8-30 range would either force padding on the short one or force
// under-coverage on the long one. It's used two ways: as the CLI's --min-slides/--max-slides
// auto-scale default, and as the comparison baseline GenerateProtoDeck's validateProtoDeck warns
// against when the actual generated count falls outside it — advisory only. This range itself is
// never passed into outline planning as a target (see GenerateProtoDeck's doc comment for why: a
// numeric target at whole-chapter scope is exactly the failure mode that made the old whole-
// chapter design silently drop topics) — generateOutlineChunked instead derives its own much
// smaller per-chunk targets from charsPerContentSlide directly (see outline_chunks.go), the same
// calibration constant this function's estimate is built on, so the two stay in sync if either is
// ever recalibrated.
//
// The range spans -15%/+25% around the point estimate rather than returning a single number,
// since it's a rough estimate from character count, not a precise measurement of how many
// distinct topics a chapter actually contains.
func AutoSlideRange(chapters []ProtoChapterInput) (minSlides, maxSlides int) {
	var totalChars int
	for i := range chapters {
		totalChars += len(chapters[i].Text)
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
// chapters (in order), plus any warnings worth surfacing to the caller (e.g. the deck's slide
// count falling outside minSlides-maxSlides — advisory only, see below). The returned markdown is
// validated for meta-marker numbering before it's returned to the caller.
//
// Generation is multi-pass rather than one-shot, on the principle that a single call asked to do
// several different jobs at once (plan every topic across a whole chapter, hit a numeric slide-
// count target, AND write full slide content) reliably does each of them worse than a call scoped
// to just one job. generateOutlineChunked plans slide topics in fixed-size chunks of each
// chapter's text, each chunk given a small explicit target slide count (see its doc comment for
// why: a single whole-chapter planning call was empirically unreliable, and so — worse — was
// asking one call per section with no numeric target at all; a small, bounded target per chunk is
// what actually holds slide count consistent run to run), generateAgenda separately frames the
// deck's title and agenda from the fully-planned topics, and expandOutline writes each topic's
// bullets in small batches, grounded in the chapter's condensed text.
//
// minSlides/maxSlides are advisory, not enforced: they still size the CLI's --min-slides/
// --max-slides default (see AutoSlideRange) and validateProtoDeck still warns when the actual
// generated count falls outside them, but generation no longer fails over it — the reconciled
// total is an emergent property of the chapter's actual length and each chunk's target, not
// something planning is asked to hit directly.
func GenerateProtoDeck(ctx context.Context, llm LLM, chapters []ProtoChapterInput, minSlides, maxSlides int) (deck string, warnings []string, err error) {
	if len(chapters) == 0 {
		return "", nil, errors.New("no chapters to build a proto deck from")
	}
	if minSlides <= 0 || maxSlides <= 0 || minSlides > maxSlides {
		return "", nil, fmt.Errorf("invalid slide range %d-%d", minSlides, maxSlides)
	}

	entries, outlineWarnings, err := generateOutlineChunked(ctx, llm, chapters)
	if err != nil {
		return "", nil, fmt.Errorf("generate outline: %w", err)
	}

	framing, err := generateAgenda(ctx, llm, chapters, entries)
	if err != nil {
		return "", nil, fmt.Errorf("generate agenda: %w", err)
	}

	outline := &outlineResponse{DeckTitle: framing.DeckTitle, Agenda: framing.Agenda, Outline: entries}
	deck, err = expandOutline(ctx, llm, chapters, outline)
	if err != nil {
		return "", nil, fmt.Errorf("expand outline: %w", err)
	}

	// Advisory only: a failure here must not throw away an otherwise-successful, already-expensive
	// generation (see reviewDeck's doc comment) — surfaced as a warning, not a returned error.
	reviewWarnings, reviewErr := reviewDeck(ctx, llm, deck)
	if reviewErr != nil {
		reviewWarnings = []string{fmt.Sprintf("automated deck review could not complete: %v", reviewErr)}
	}

	validateWarnings, err := validateProtoDeck(deck, minSlides, maxSlides)
	if err != nil {
		return "", nil, fmt.Errorf("invalid proto deck: %w", err)
	}
	warnings = append(warnings, outlineWarnings...)
	warnings = append(warnings, reviewWarnings...)
	warnings = append(warnings, validateWarnings...)
	return deck, warnings, nil
}

// validateProtoDeck checks that deck's <!-- meta: N tag --> markers are sequential starting at 1,
// with no gaps or repeats (a hard error — a numbering gap indicates broken generation, unrelated
// to how much content was planned) and returns a warning, not an error, when the slide count
// falls outside [minSlides, maxSlides] (advisory only — see GenerateProtoDeck's doc comment).
func validateProtoDeck(deck string, minSlides, maxSlides int) (warnings []string, err error) {
	matches := reProtoMeta.FindAllStringSubmatch(deck, -1)
	if got := len(matches); got < minSlides || got > maxSlides {
		warnings = append(warnings, fmt.Sprintf("deck has %d slides, outside the requested %d-%d range", got, minSlides, maxSlides))
	}
	for i, m := range matches {
		want := i + 1
		got, convErr := strconv.Atoi(m[1])
		if convErr != nil || got != want {
			return nil, fmt.Errorf("slide meta numbers must be sequential starting at 1: expected %d, got %q", want, m[1])
		}
	}
	return warnings, nil
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
