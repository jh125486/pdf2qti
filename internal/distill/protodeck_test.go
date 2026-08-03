package distill_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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
	reTargetContentRange = regexp.MustCompile(`TARGET_CONTENT_RANGE: min=(\d+) max=(\d+)`)
	rePlannedSlideLine   = regexp.MustCompile(`(?m)^\d+\. \[`)
	reChapterHeading     = regexp.MustCompile(`(?m)^### (\S+):`)
)

// protoDeckStubLLM answers GenerateProtoDeck's three prompt shapes (outline, batch expansion,
// summary) — distinguished by content, like stubModuleLLM in cmd/pdf2qti/commands/module.go does
// — plus BuildModuleDoc's JSON-merge prompt, so tests can exercise the full two-pass pipeline
// without a real LLM. outlineCount, if non-zero, overrides how many outline entries are
// returned, to test under/over-shoot handling; zero means "use the prompt's own requested min".
type protoDeckStubLLM struct {
	err          error
	outlineCount int
	calls        []string // records which shape each call was, in order
}

func (s *protoDeckStubLLM) Complete(_ context.Context, prompt string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	switch {
	case strings.Contains(prompt, "planning a prototype PowerPoint outline"):
		s.calls = append(s.calls, "outline")
		return s.stubOutline(prompt), nil
	case strings.Contains(prompt, "writing the bullet content for"):
		s.calls = append(s.calls, "expand")
		return stubExpandBatch(prompt), nil
	case strings.Contains(prompt, "exactly one summary bullet per agenda item"):
		s.calls = append(s.calls, "summary")
		return stubSummary(prompt), nil
	default:
		s.calls = append(s.calls, "merge")
		return `{"overview":"","objectives":[],"vocabulary":[],"theorems":[]}`, nil
	}
}

func (s *protoDeckStubLLM) stubOutline(prompt string) string {
	n := 1
	if m := reTargetContentRange.FindStringSubmatch(prompt); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			n = v
		}
	}
	if s.outlineCount > 0 {
		n = s.outlineCount
	}
	entries := make([]string, n)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"tag":"ch1","title":"Slide %d","focus":"f"}`, i+1)
	}
	return fmt.Sprintf(`{"deck_title":"Deck","agenda":["a","b","c"],"outline":[%s]}`, strings.Join(entries, ","))
}

func stubExpandBatch(prompt string) string {
	n := len(rePlannedSlideLine.FindAllString(prompt, -1))
	slides := make([]string, n)
	for i := range slides {
		slides[i] = `{"bullets":["a"]}`
	}
	return fmt.Sprintf(`{"slides":[%s]}`, strings.Join(slides, ","))
}

func stubSummary(prompt string) string {
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
		name      string
		llm       *protoDeckStubLLM
		chapters  []distill.ProtoChapterInput
		minSlides int
		maxSlides int
		wantErr   bool
		errLike   string
	}{
		{name: "happy path", llm: &protoDeckStubLLM{}, chapters: chapters, minSlides: 3, maxSlides: 8},
		{name: "no chapters", llm: &protoDeckStubLLM{}, chapters: nil, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "no chapters"},
		{name: "invalid slide range", llm: &protoDeckStubLLM{}, chapters: chapters, minSlides: 8, maxSlides: 3, wantErr: true, errLike: "invalid slide range"},
		{name: "llm error", llm: &protoDeckStubLLM{err: fmt.Errorf("boom")}, chapters: chapters, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "llm complete"},
		{
			name: "outline undershoots minimum", llm: &protoDeckStubLLM{outlineCount: 1}, chapters: chapters,
			minSlides: 5, maxSlides: 8, wantErr: true, errLike: "need at least",
		},
		{
			// minContent=6 (maxSlides 8 - 2); outline returns 20, must be truncated to fit, not
			// error out.
			name: "outline overshoots maximum gets truncated", llm: &protoDeckStubLLM{outlineCount: 20}, chapters: chapters,
			minSlides: 3, maxSlides: 8,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := distill.GenerateProtoDeck(context.Background(), tt.llm, tt.chapters, tt.minSlides, tt.maxSlides)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if !tt.wantErr && got == "" {
				t.Fatal("expected non-empty deck")
			}
		})
	}
}

func TestGenerateProtoDeck_CallsOutlineThenExpandThenSummary(t *testing.T) {
	t.Parallel()

	chapters := []distill.ProtoChapterInput{{Tag: "ch1", ModuleName: "Signals", Overview: "o", Text: "t"}}
	llm := &protoDeckStubLLM{outlineCount: 8}
	_, err := distill.GenerateProtoDeck(context.Background(), llm, chapters, 3, 12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.calls) < 3 {
		t.Fatalf("expected at least 3 calls (outline, >=1 expand batch, summary), got %v", llm.calls)
	}
	if llm.calls[0] != "outline" {
		t.Fatalf("expected first call to be outline, got %v", llm.calls)
	}
	if llm.calls[len(llm.calls)-1] != "summary" {
		t.Fatalf("expected last call to be summary, got %v", llm.calls)
	}
	for _, c := range llm.calls[1 : len(llm.calls)-1] {
		if c != "expand" {
			t.Fatalf("expected all calls between outline and summary to be expand, got %v", llm.calls)
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
