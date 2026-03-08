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
