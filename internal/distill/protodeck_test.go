package distill_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
)

func validDeck(n int) string {
	var b strings.Builder
	b.WriteString("# Deck\n\n---\n\n<!-- meta: 1 agenda -->\n# Agenda\n\n- a\n\n---\n\n")
	for i := 2; i < n; i++ {
		fmt.Fprintf(&b, "<!-- meta: %d ch1 -->\n# Slide %d\n\n- a\n\n---\n\n", i, i)
	}
	fmt.Fprintf(&b, "<!-- meta: %d summary -->\n# Summary\n\n- a\n", n)
	return b.String()
}

var (
	rePlannedSlideLine = regexp.MustCompile(`(?m)^\d+\. \[`)
	reReconcileLine    = regexp.MustCompile(`(?m)^\d+\. \[([^\]]*)\] (.*) — (.*)$`)
	reChapterHeading   = regexp.MustCompile(`(?m)^### (\S+):`)
)

// protoDeckStubLLM answers GenerateProtoDeck's prompt shapes (per-chunk outline planning,
// outline reconciliation, agenda/title framing, batch expansion, summary) — distinguished by
// content, like stubProtoDeckShape in cmd/pdf2qti/commands/llm.go does — plus BuildModuleDoc's
// JSON-merge prompt, so tests can exercise the full pipeline without a real LLM.
// chunkOutlineCount is the number of entries stubChunkOutline returns per chunk-outline call (0
// legitimately means "this chunk has no distinct topics," used to test the no-topics-at-all
// hard-failure path). injectUnknownTagInReconcile adds one extra entry with a tag no chapter has
// to the reconcile response, to test reconcileOutline's defensive drop-and-warn path for a
// hallucinated tag. reconcileEntryAsRawString appends one bare JSON string (instead of a
// {tag,title,focus} object) to the reconcile response's outline array, to test
// outlineEntry.UnmarshalJSON's string-fallback path. agendaBulletCount, if nonzero, overrides
// how many agenda bullets stubAgenda returns, to test generateAgenda's 3-8 bullet-count
// validation. summaryEmpty makes stubSummary return zero bullets, to test expandSummary's
// no-bullets error. expandEmptyFirstCall makes the first expand-batch call return zero slide
// entries (a schema-valid but empty response, observed in practice against the real OpenAI API)
// to test expandBatch's retry-once behavior; every subsequent expand call succeeds normally.
type protoDeckStubLLM struct {
	err                         error
	chunkOutlineCount           int
	injectUnknownTagInReconcile bool
	reconcileEntryAsRawString   bool
	agendaBulletCount           int
	summaryEmpty                bool
	expandEmptyFirstCall        bool
	expandCalls                 int
	calls                       []string // records which shape each call was, in order
}

func (s *protoDeckStubLLM) Complete(_ context.Context, prompt string, _ *distill.Schema) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	switch {
	case strings.Contains(prompt, "## This excerpt (part "):
		s.calls = append(s.calls, "chunk-outline")
		return s.stubChunkOutline(), nil
	case strings.Contains(prompt, "reviewing a slide outline that was planned in fixed-size chunks of chapter text"):
		s.calls = append(s.calls, "reconcile")
		return s.stubReconcileOutline(prompt), nil
	case strings.Contains(prompt, "writing the overall title and agenda for a prototype PowerPoint deck"):
		s.calls = append(s.calls, "agenda")
		return s.stubAgenda(), nil
	case strings.Contains(prompt, "writing the bullet content for"):
		s.calls = append(s.calls, "expand")
		return s.stubExpandBatch(prompt), nil
	case strings.Contains(prompt, "exactly one summary bullet per agenda item"):
		s.calls = append(s.calls, "summary")
		return s.stubSummary(prompt), nil
	default:
		s.calls = append(s.calls, "merge")
		return `{"overview":"","objectives":[],"vocabulary":[],"theorems":[]}`, nil
	}
}

func (s *protoDeckStubLLM) stubChunkOutline() string {
	entries := make([]string, s.chunkOutlineCount)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"title":"Slide %d","focus":"f"}`, i+1)
	}
	return fmt.Sprintf(`{"outline":[%s]}`, strings.Join(entries, ","))
}

// stubReconcileOutline echoes back the joined outline entries the reconcile prompt renders
// (one "N. [tag] Title — Focus" line per entry), unmerged — a legitimately valid "no duplicates
// found" response, since these stub-driven entries never actually duplicate each other. Echoes
// the tag back WITH its surrounding brackets ("[ch01]" rather than "ch01"), matching real
// observed behavior from the OpenAI API — this exercises reconcileOutline's defensive
// bracket-stripping on every table case using this stub, not just a dedicated one, guarding
// against a regression of the bug it fixed (every real-API run failing "no slide topics after
// reconciliation" because no returned tag matched knownTags until brackets were stripped). When
// injectUnknownTagInReconcile is set, appends one extra entry tagged with a chapter tag that
// doesn't exist even after stripping, to test reconcileOutline's drop-and-warn path.
func (s *protoDeckStubLLM) stubReconcileOutline(prompt string) string {
	matches := reReconcileLine.FindAllStringSubmatch(prompt, -1)
	entries := make([]string, 0, len(matches)+1)
	for _, m := range matches {
		entries = append(entries, fmt.Sprintf(`{"tag":"[%s]","title":%q,"focus":%q}`, m[1], m[2], m[3]))
	}
	if s.injectUnknownTagInReconcile {
		entries = append(entries, `{"tag":"bogus-tag","title":"Ghost","focus":"f"}`)
	}
	if s.reconcileEntryAsRawString {
		entries = append(entries, `"just a title string"`)
	}
	return fmt.Sprintf(`{"outline":[%s],"warnings":[]}`, strings.Join(entries, ","))
}

// stubAgenda returns a fixed 3-bullet agenda by default, or agendaBulletCount bullets when set,
// to test generateAgenda's 3-8 bullet-count validation.
func (s *protoDeckStubLLM) stubAgenda() string {
	n := 3
	if s.agendaBulletCount != 0 {
		n = s.agendaBulletCount
	}
	bullets := make([]string, n)
	for i := range bullets {
		bullets[i] = fmt.Sprintf(`"item %d"`, i+1)
	}
	return fmt.Sprintf(`{"deck_title":"Deck","agenda":[%s]}`, strings.Join(bullets, ","))
}

func (s *protoDeckStubLLM) stubExpandBatch(prompt string) string {
	s.expandCalls++
	if s.expandEmptyFirstCall && s.expandCalls == 1 {
		return `{"slides":[]}`
	}
	n := len(rePlannedSlideLine.FindAllString(prompt, -1))
	slides := make([]string, n)
	for i := range slides {
		slides[i] = `{"bullets":["a"]}`
	}
	return fmt.Sprintf(`{"slides":[%s]}`, strings.Join(slides, ","))
}

// stubSummary returns one bullet per chapter heading in prompt (a fixed 1 if empty), or zero
// bullets when summaryEmpty is set, to test expandSummary's no-bullets error.
func (s *protoDeckStubLLM) stubSummary(prompt string) string {
	if s.summaryEmpty {
		return `{"bullets":[]}`
	}
	n := len(reChapterHeading.FindAllString(prompt, -1))
	if n == 0 {
		n = 1
	}
	bullets := make([]string, n)
	for i := range bullets {
		bullets[i] = `"a"`
	}
	return fmt.Sprintf(`{"bullets":[%s]}`, strings.Join(bullets, ","))
}

func TestGenerateProtoDeck_Table(t *testing.T) {
	t.Parallel()

	chapters := []distill.ProtoChapterInput{{Tag: "ch1", ModuleName: "Signals", Overview: "o", Text: "t"}}

	tests := []struct {
		name         string
		llm          *protoDeckStubLLM
		chapters     []distill.ProtoChapterInput
		minSlides    int
		maxSlides    int
		wantErr      bool
		errLike      string
		wantWarnLike string // if set, at least one returned warning must contain this
	}{
		{name: "happy path", llm: &protoDeckStubLLM{chunkOutlineCount: 3}, chapters: chapters, minSlides: 3, maxSlides: 8},
		{name: "no chapters", llm: &protoDeckStubLLM{}, chapters: nil, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "no chapters"},
		{name: "invalid slide range", llm: &protoDeckStubLLM{}, chapters: chapters, minSlides: 8, maxSlides: 3, wantErr: true, errLike: "invalid slide range"},
		{name: "llm error", llm: &protoDeckStubLLM{err: fmt.Errorf("boom")}, chapters: chapters, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "llm complete"},
		{
			// The chapter's Text ("t") is short enough to produce exactly one chunk; that
			// chunk's stub outline call returns zero entries, so the joined list across the
			// whole chapter is empty before reconciliation is ever attempted.
			name: "no topics in any chunk is a hard failure", llm: &protoDeckStubLLM{chunkOutlineCount: 0}, chapters: chapters,
			minSlides: 5, maxSlides: 8, wantErr: true, errLike: "no slide topics",
		},
		{
			// Each chunk gets a small target, but there's no chapter-level cap on how many
			// chunks a chapter has or how their per-chunk counts sum — a single-chunk count
			// this high is unrealistic in practice but exercises the same "advisory, not
			// truncated" path a multi-chunk chapter's summed count could hit for real.
			name: "count above max is a warning, not truncated", llm: &protoDeckStubLLM{chunkOutlineCount: 20}, chapters: chapters,
			minSlides: 3, maxSlides: 8, wantWarnLike: "outside the requested 3-8 range",
		},
		{
			// Deck total = 1 agenda + 1 content (single chunk, 1 entry) + 1 summary = 3, outside
			// the requested 2-2 range — advisory warning, not the hard failure this used to be
			// before slide-count enforcement became advisory-only.
			name: "count outside range is a warning, not a hard failure", llm: &protoDeckStubLLM{chunkOutlineCount: 1}, chapters: chapters,
			minSlides: 2, maxSlides: 2, wantWarnLike: "deck has 3 slides, outside the requested 2-2 range",
		},
		{
			// reconcileOutline must defensively drop (not trust) any returned entry whose tag
			// doesn't match a known chapter, in case the LLM hallucinates one during merging.
			name: "reconcile entry with unknown tag is dropped as a warning", llm: &protoDeckStubLLM{chunkOutlineCount: 1, injectUnknownTagInReconcile: true}, chapters: chapters,
			minSlides: 3, maxSlides: 8, wantWarnLike: `unknown tag "bogus-tag", dropped`,
		},
		{
			// outlineEntry.UnmarshalJSON's bare-string fallback (Tag left empty) must not crash
			// reconcileOutline's parse — the resulting empty tag is then correctly dropped by the
			// same knownTags check as any other unrecognized tag.
			name: "reconcile entry as bare string is tolerated and dropped as a warning", llm: &protoDeckStubLLM{chunkOutlineCount: 1, reconcileEntryAsRawString: true}, chapters: chapters,
			minSlides: 3, maxSlides: 8, wantWarnLike: `unknown tag ""`,
		},
		{
			// generateAgenda validates the model returned 3-8 agenda bullets, same as the old
			// whole-chapter design's agenda validation.
			name: "agenda with too few bullets is an error", llm: &protoDeckStubLLM{chunkOutlineCount: 1, agendaBulletCount: 2}, chapters: chapters,
			minSlides: 3, maxSlides: 8, wantErr: true, errLike: "agenda has 2 bullets, need 3-8",
		},
		{
			// expandSummary requires at least one summary bullet.
			name: "empty summary is an error", llm: &protoDeckStubLLM{chunkOutlineCount: 1, summaryEmpty: true}, chapters: chapters,
			minSlides: 3, maxSlides: 8, wantErr: true, errLike: "summary has no bullets",
		},
		{
			// expandBatch retries once when the model returns a schema-valid but empty slides
			// response (observed in practice against the real OpenAI API) — the retry succeeds
			// and generation completes normally rather than failing on the first empty response.
			name: "expand batch retries once on empty response", llm: &protoDeckStubLLM{chunkOutlineCount: 1, expandEmptyFirstCall: true}, chapters: chapters,
			minSlides: 3, maxSlides: 8,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, warnings, err := distill.GenerateProtoDeck(t.Context(), tt.llm, tt.chapters, tt.minSlides, tt.maxSlides)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if !tt.wantErr && got == "" {
				t.Fatal("expected non-empty deck")
			}
			if tt.wantWarnLike != "" {
				found := false
				for _, w := range warnings {
					if strings.Contains(w, tt.wantWarnLike) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected a warning containing %q, got %v", tt.wantWarnLike, warnings)
				}
			}
		})
	}
}

// TestGenerateProtoDeck_CallSequence asserts the exact call sequence for a chapter whose Text
// splits into 3 chunks (one chunk-outline call each, in order, then one reconcile call, one
// agenda call, then expand batches, then summary) — a structurally different assertion (call
// order, not error/warning shape) from TestGenerateProtoDeck_Table, so it's kept as its own
// function rather than folded into that table.
func TestGenerateProtoDeck_CallSequence(t *testing.T) {
	t.Parallel()

	// 3 paragraphs of 3000 chars each: splitIntoChunks (chunk.go), given outlineChunkChars =
	// 4000, packs at most one 3000-char paragraph per chunk (two together would exceed 4000),
	// so this deterministically produces exactly 3 chunks.
	text := strings.Repeat("a", 3000) + "\n\n" + strings.Repeat("b", 3000) + "\n\n" + strings.Repeat("c", 3000)
	chapters := []distill.ProtoChapterInput{{Tag: "ch1", ModuleName: "Signals", Overview: "o", Text: text}}
	llm := &protoDeckStubLLM{chunkOutlineCount: 3}
	_, _, err := distill.GenerateProtoDeck(t.Context(), llm, chapters, 3, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPrefix := []string{"chunk-outline", "chunk-outline", "chunk-outline", "reconcile", "agenda"}
	if len(llm.calls) < len(wantPrefix)+1 {
		t.Fatalf("expected at least %d calls, got %v", len(wantPrefix)+1, llm.calls)
	}
	for i, want := range wantPrefix {
		if llm.calls[i] != want {
			t.Fatalf("call %d: got %q, want %q (full sequence: %v)", i, llm.calls[i], want, llm.calls)
		}
	}
	if llm.calls[len(llm.calls)-1] != "summary" {
		t.Fatalf("expected last call to be summary, got %v", llm.calls)
	}
	for _, c := range llm.calls[len(wantPrefix) : len(llm.calls)-1] {
		if c != "expand" {
			t.Fatalf("expected all calls between agenda and summary to be expand, got %v", llm.calls)
		}
	}
}

func TestParseProtoDeck_RoundTripsGenerateProtoDeck(t *testing.T) {
	t.Parallel()

	title, agenda, slides, err := distill.ParseProtoDeck(validDeck(5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Deck" {
		t.Fatalf("got title %q, want %q", title, "Deck")
	}
	if len(agenda) != 1 || agenda[0] != "a" {
		t.Fatalf("got agenda %v, want [\"a\"]", agenda)
	}
	wantSlides := []distill.Slide{
		{Title: "Slide 2", Content: "a", Tag: "ch1"},
		{Title: "Slide 3", Content: "a", Tag: "ch1"},
		{Title: "Slide 4", Content: "a", Tag: "ch1"},
		{Title: "Summary", Content: "a", Tag: "summary"},
	}
	if len(slides) != len(wantSlides) {
		t.Fatalf("got %d slides, want %d: %+v", len(slides), len(wantSlides), slides)
	}
	for i, want := range wantSlides {
		if slides[i] != want {
			t.Fatalf("slide %d: got %+v, want %+v", i, slides[i], want)
		}
	}
}

func TestParseProtoDeck_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "empty markdown", in: "", wantErr: true},
		{name: "no meta markers at all", in: "# Just A Title\n\nSome prose, no slides.\n", wantErr: true},
		{
			name: "hand-written deck, loose spacing",
			in: "# My Deck\n---\n<!-- meta: 1 agenda -->\n# Agenda\n- one\n- two\n---\n" +
				"<!-- meta: 2 ch01 -->\n# A Slide\n- bullet\n",
		},
		{
			// A stray block with no meta marker at all (e.g. leftover prose between separators)
			// must be silently skipped, not break parsing of the valid blocks around it.
			name: "stray block without meta marker is skipped",
			in: "# My Deck\n---\n<!-- meta: 1 agenda -->\n# Agenda\n- one\n- two\n---\n" +
				"Some stray prose with no meta marker at all.\n---\n" +
				"<!-- meta: 2 ch01 -->\n# A Slide\n- bullet\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, _, err := distill.ParseProtoDeck(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestParseProtoDeck_HandWrittenLooseSpacing(t *testing.T) {
	t.Parallel()

	in := "# My Deck\n---\n<!-- meta: 1 agenda -->\n# Agenda\n- one\n- two\n---\n" +
		"<!-- meta: 2 ch01 -->\n# A Slide\n- bullet\n"

	title, agenda, slides, err := distill.ParseProtoDeck(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "My Deck" {
		t.Fatalf("got title %q, want %q", title, "My Deck")
	}
	if len(agenda) != 2 || agenda[0] != "one" || agenda[1] != "two" {
		t.Fatalf("got agenda %v", agenda)
	}
	if len(slides) != 1 || slides[0] != (distill.Slide{Title: "A Slide", Content: "bullet", Tag: "ch01"}) {
		t.Fatalf("got slides %+v", slides)
	}
}

func TestAutoSlideRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		chapters []distill.ProtoChapterInput
		wantMin  int
		wantMax  int
	}{
		{
			name:     "no chapters",
			chapters: nil,
			wantMin:  8, // minContentSlides(6) + 2
			wantMax:  8, // target 0 clamps to minContentSlides on both ends
		},
		{
			name:     "thin chapter clamps to floor",
			chapters: []distill.ProtoChapterInput{{Text: strings.Repeat("x", 200)}},
			wantMin:  8,
			wantMax:  8,
		},
		{
			name: "calibration chapter (~14.2K chars, 9-section zyBooks chapter)",
			// Real distilled ch01: 9 sections, ~94-page source, condenses to ~14.2K chars.
			// target = 14200/400 = 35; min = floor(35*0.85) = 29; max = ceil-ish(35*1.25) = 43.
			chapters: []distill.ProtoChapterInput{{Text: strings.Repeat("x", 14200)}},
			wantMin:  31, // 35*85/100 (int division) + 2
			wantMax:  45, // 35*125/100 + 2
		},
		{
			name: "multiple chapters sum text length",
			chapters: []distill.ProtoChapterInput{
				{Text: strings.Repeat("x", 7100)},
				{Text: strings.Repeat("x", 7100)},
			},
			wantMin: 31,
			wantMax: 45,
		},
		{
			name:     "huge chapter clamps to ceiling",
			chapters: []distill.ProtoChapterInput{{Text: strings.Repeat("x", 10_000_000)}},
			wantMin:  122, // maxContentSlides(120) + 2
			wantMax:  122,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMin, gotMax := distill.AutoSlideRange(tt.chapters)
			if gotMin != tt.wantMin || gotMax != tt.wantMax {
				t.Fatalf("AutoSlideRange() = (%d, %d), want (%d, %d)", gotMin, gotMax, tt.wantMin, tt.wantMax)
			}
			if gotMin > gotMax {
				t.Fatalf("AutoSlideRange() min %d > max %d", gotMin, gotMax)
			}
		})
	}
}
