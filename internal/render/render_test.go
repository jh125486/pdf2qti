package render_test

import (
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/render"
)

func TestRenderDraft(t *testing.T) {
	draft := &render.QuizDraft{
		Title:       "Test Quiz",
		Description: "A test description",
		TFQuestions: []render.Question{
			{Number: 1, Text: "Is the sky blue?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: false},
			}},
		},
		MAQuestions: []render.Question{
			{Number: 2, Text: "Which are colors?", Options: []render.Option{
				{Text: "Red", IsCorrect: true},
				{Text: "Dog", IsCorrect: false},
				{Text: "Blue", IsCorrect: true},
			}},
		},
		MCQuestions: []render.Question{
			{Number: 3, Text: "What is 2+2?", Options: []render.Option{
				{Text: "3", IsCorrect: false},
				{Text: "4", IsCorrect: true},
				{Text: "5", IsCorrect: false},
			}},
		},
	}

	md, err := render.RenderDraft(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(md, "# Test Quiz") {
		t.Error("missing title")
	}
	if !strings.Contains(md, "A test description") {
		t.Error("missing description")
	}
	if !strings.Contains(md, "## TF") {
		t.Error("missing TF section")
	}
	if !strings.Contains(md, "## MA") {
		t.Error("missing MA section")
	}
	if !strings.Contains(md, "## MC") {
		t.Error("missing MC section")
	}
	if !strings.Contains(md, "[*] True") {
		t.Error("missing correct TF answer")
	}
	if !strings.Contains(md, "[ ] False") {
		t.Error("missing wrong TF answer")
	}
}

func TestRenderDraft_NoDescription(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "No Desc",
		MCQuestions: []render.Question{
			{Number: 1, Text: "Q?", Options: []render.Option{{Text: "A", IsCorrect: true}}},
		},
	}
	md, err := render.RenderDraft(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(md, "# No Desc") {
		t.Error("missing title")
	}
}

func TestParseDraft_RoundTrip(t *testing.T) {
	original := &render.QuizDraft{
		Title:       "Round Trip Quiz",
		Description: "Description here",
		TFQuestions: []render.Question{
			{Number: 1, Text: "TF question one?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: false},
			}},
		},
		MCQuestions: []render.Question{
			{Number: 2, Text: "MC question two?", Options: []render.Option{
				{Text: "A", IsCorrect: true},
				{Text: "B", IsCorrect: false},
			}},
		},
	}

	md, err := render.RenderDraft(original)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}

	parsed, err := render.ParseDraft(md)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.Title != original.Title {
		t.Errorf("title: got %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Description != original.Description {
		t.Errorf("description: got %q, want %q", parsed.Description, original.Description)
	}
	if len(parsed.TFQuestions) != len(original.TFQuestions) {
		t.Errorf("TF count: got %d, want %d", len(parsed.TFQuestions), len(original.TFQuestions))
	}
	if len(parsed.MCQuestions) != len(original.MCQuestions) {
		t.Errorf("MC count: got %d, want %d", len(parsed.MCQuestions), len(original.MCQuestions))
	}
	if len(parsed.TFQuestions) > 0 {
		if parsed.TFQuestions[0].Text != original.TFQuestions[0].Text {
			t.Errorf("TF[0] text: got %q, want %q", parsed.TFQuestions[0].Text, original.TFQuestions[0].Text)
		}
	}
}

func TestRenderDraft_NewTypes(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "New Types Quiz",
		SAQuestions: []render.Question{
			{Number: 1, Text: "The capital of France is ___.", Options: []render.Option{
				{Text: "Paris", IsCorrect: true},
			}},
		},
		ESQuestions: []render.Question{
			{Number: 2, Text: "Describe the water cycle."},
		},
		MTQuestions: []render.Question{
			{Number: 3, Text: "Match each country to its capital.", Options: []render.Option{
				{Text: "France", IsCorrect: true, MatchText: "Paris"},
				{Text: "Germany", IsCorrect: true, MatchText: "Berlin"},
			}},
		},
		NRQuestions: []render.Question{
			{Number: 4, Text: "What is 2+2?", Options: []render.Option{
				{Text: "4", IsCorrect: true},
				{Text: "0.5", IsCorrect: false},
			}},
		},
	}

	md, err := render.RenderDraft(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"## SA", "[=] Paris",
		"## ES", "Describe the water cycle.",
		"## MT", "[>] France = Paris", "[>] Germany = Berlin",
		"## NR", "[=] 4", "[~] 0.5",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in rendered output", want)
		}
	}
}

func TestParseDraft_RoundTrip_NewTypes(t *testing.T) {
	original := &render.QuizDraft{
		Title: "New Types Round Trip",
		SAQuestions: []render.Question{
			{Number: 1, Text: "Capital of Italy?", Options: []render.Option{
				{Text: "Rome", IsCorrect: true},
			}},
		},
		ESQuestions: []render.Question{
			{Number: 2, Text: "Explain photosynthesis."},
		},
		MTQuestions: []render.Question{
			{Number: 3, Text: "Match the pairs.", Options: []render.Option{
				{Text: "A", IsCorrect: true, MatchText: "1"},
				{Text: "B", IsCorrect: true, MatchText: "2"},
			}},
		},
		NRQuestions: []render.Question{
			{Number: 4, Text: "Value of π rounded to 2 decimals?", Options: []render.Option{
				{Text: "3.14", IsCorrect: true},
				{Text: "0.005", IsCorrect: false},
			}},
		},
	}

	md, err := render.RenderDraft(original)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	parsed, err := render.ParseDraft(md)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(parsed.SAQuestions) != 1 {
		t.Errorf("SA count: got %d, want 1", len(parsed.SAQuestions))
	} else if parsed.SAQuestions[0].Text != original.SAQuestions[0].Text {
		t.Errorf("SA text: got %q, want %q", parsed.SAQuestions[0].Text, original.SAQuestions[0].Text)
	} else if len(parsed.SAQuestions[0].Options) != 1 || parsed.SAQuestions[0].Options[0].Text != "Rome" {
		t.Errorf("SA option: got %v", parsed.SAQuestions[0].Options)
	}

	if len(parsed.ESQuestions) != 1 {
		t.Errorf("ES count: got %d, want 1", len(parsed.ESQuestions))
	} else if parsed.ESQuestions[0].Text != original.ESQuestions[0].Text {
		t.Errorf("ES text: got %q, want %q", parsed.ESQuestions[0].Text, original.ESQuestions[0].Text)
	}

	if len(parsed.MTQuestions) != 1 {
		t.Errorf("MT count: got %d, want 1", len(parsed.MTQuestions))
	} else {
		pairs := parsed.MTQuestions[0].Options
		if len(pairs) != 2 {
			t.Errorf("MT pairs count: got %d, want 2", len(pairs))
		} else {
			if pairs[0].Text != "A" || pairs[0].MatchText != "1" {
				t.Errorf("MT pair 0: got (%q, %q), want (A, 1)", pairs[0].Text, pairs[0].MatchText)
			}
			if pairs[1].Text != "B" || pairs[1].MatchText != "2" {
				t.Errorf("MT pair 1: got (%q, %q), want (B, 2)", pairs[1].Text, pairs[1].MatchText)
			}
		}
	}

	if len(parsed.NRQuestions) != 1 {
		t.Errorf("NR count: got %d, want 1", len(parsed.NRQuestions))
	} else {
		opts := parsed.NRQuestions[0].Options
		if len(opts) != 2 {
			t.Errorf("NR options count: got %d, want 2", len(opts))
		} else {
			if opts[0].Text != "3.14" || !opts[0].IsCorrect {
				t.Errorf("NR answer: got (%q, isCorrect=%v), want (3.14, true)", opts[0].Text, opts[0].IsCorrect)
			}
			if opts[1].Text != "0.005" || opts[1].IsCorrect {
				t.Errorf("NR tolerance: got (%q, isCorrect=%v), want (0.005, false)", opts[1].Text, opts[1].IsCorrect)
			}
		}
	}
}

func TestExecuteTemplate_Valid(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		data     any
		expected string
	}{
		{
			name:     "simple map",
			tmpl:     "Chapter {{.chapter}}",
			data:     map[string]any{"chapter": 5},
			expected: "Chapter 5",
		},
		{
			name:     "no vars",
			tmpl:     "Hello World",
			data:     nil,
			expected: "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := render.ExecuteTemplate(tt.tmpl, tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExecuteTemplate_InvalidTemplate(t *testing.T) {
	_, err := render.ExecuteTemplate("{{.unclosed", nil)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}
