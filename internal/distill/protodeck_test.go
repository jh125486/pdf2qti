package distill_test

import (
	"context"
	"fmt"
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

func TestGenerateProtoDeck_Table(t *testing.T) {
	t.Parallel()

	chapters := []distill.ProtoChapterInput{{Tag: "ch1", ModuleName: "Signals", Overview: "o", Text: "t"}}

	tests := []struct {
		name      string
		llm       *stubLLM
		chapters  []distill.ProtoChapterInput
		minSlides int
		maxSlides int
		wantErr   bool
		errLike   string
	}{
		{name: "happy path", llm: &stubLLM{response: validDeck(5)}, chapters: chapters, minSlides: 3, maxSlides: 8},
		{name: "no chapters", llm: &stubLLM{response: validDeck(5)}, chapters: nil, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "no chapters"},
		{name: "invalid slide range", llm: &stubLLM{response: validDeck(5)}, chapters: chapters, minSlides: 8, maxSlides: 3, wantErr: true, errLike: "invalid slide range"},
		{name: "llm error", llm: &stubLLM{err: fmt.Errorf("boom")}, chapters: chapters, minSlides: 3, maxSlides: 8, wantErr: true, errLike: "llm complete"},
		{name: "too few slides", llm: &stubLLM{response: validDeck(2)}, chapters: chapters, minSlides: 5, maxSlides: 8, wantErr: true, errLike: "expected between"},
		{
			name:     "non-sequential meta markers",
			llm:      &stubLLM{response: "<!-- meta: 1 agenda -->\n# Agenda\n\n---\n\n<!-- meta: 3 summary -->\n# Summary\n"},
			chapters: chapters, minSlides: 1, maxSlides: 5,
			wantErr: true, errLike: "sequential",
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
		{Title: "Slide 2", Content: "a"},
		{Title: "Slide 3", Content: "a"},
		{Title: "Slide 4", Content: "a"},
		{Title: "Summary", Content: "a"},
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
	if len(slides) != 1 || slides[0] != (distill.Slide{Title: "A Slide", Content: "bullet"}) {
		t.Fatalf("got slides %+v", slides)
	}
}
