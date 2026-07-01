package qti_test

import (
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

func sampleDraft() *render.QuizDraft {
	return &render.QuizDraft{
		Title: "Signals Quiz",
		TFQuestions: []render.Question{
			{Number: 1, Text: "Signals are synchronous by default?", Options: []render.Option{{Text: "True", IsCorrect: false}, {Text: "False", IsCorrect: true}}},
		},
		MAQuestions: []render.Question{
			{Number: 2, Text: "Select async-signal-safe operations", Options: []render.Option{{Text: "write", IsCorrect: true}, {Text: "printf", IsCorrect: false}}},
		},
		MCQuestions: []render.Question{
			{Number: 3, Text: "Ctrl-C sends?", Options: []render.Option{{Text: "SIGINT", IsCorrect: true}, {Text: "SIGTERM", IsCorrect: false}}},
		},
		SAQuestions: []render.Question{
			{Number: 4, Text: "Name one ignored signal", Options: []render.Option{{Text: "SIGPIPE", IsCorrect: true}}},
		},
		ESQuestions: []render.Question{
			{Number: 5, Text: "Explain signal dispositions."},
		},
		MTQuestions: []render.Question{
			{Number: 6, Text: "Match signal to action", Options: []render.Option{{Text: "SIGINT", MatchText: "Terminate", IsCorrect: true}}},
		},
		NRQuestions: []render.Question{
			{Number: 7, Text: "How many realtime signals?", Options: []render.Option{{Text: "32", IsCorrect: true}, {Text: "1", IsCorrect: false}}},
		},
	}
}

func TestBuildAssessment_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		draft   *render.QuizDraft
		wantErr bool
		errLike string
	}{
		{name: "success", draft: sampleDraft()},
		{name: "missing title", draft: &render.QuizDraft{}, wantErr: true, errLike: "must have a title"},
		{name: "bad tf no correct option", draft: &render.QuizDraft{Title: "Bad", TFQuestions: []render.Question{{Number: 1, Text: "Q", Options: []render.Option{{Text: "A", IsCorrect: false}}}}}, wantErr: true, errLike: "has no correct option"},
		{
			name: "MA with multiple correct answers",
			draft: &render.QuizDraft{Title: "MA Multi", MAQuestions: []render.Question{
				{Number: 1, Text: "Pick two", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: true}, {Text: "C", IsCorrect: false},
				}},
			}},
		},
		{
			name:    "SA no options",
			draft:   &render.QuizDraft{Title: "SA Bad", SAQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no accepted answers",
		},
		{
			name:    "MT no options",
			draft:   &render.QuizDraft{Title: "MT Bad", MTQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no pairs",
		},
		{
			name:    "NR no options",
			draft:   &render.QuizDraft{Title: "NR Bad", NRQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no answer",
		},
		{
			name: "NR without tolerance",
			draft: &render.QuizDraft{Title: "NR Single", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "42", IsCorrect: true}}},
			}},
		},
		{
			name: "NR invalid answer value",
			draft: &render.QuizDraft{Title: "NR Bad Answer", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "not-a-number", IsCorrect: true}, {Text: "1", IsCorrect: false}}},
			}},
			wantErr: true, errLike: "invalid answer value",
		},
		{
			name: "NR invalid tolerance value",
			draft: &render.QuizDraft{Title: "NR Bad Tolerance", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "42", IsCorrect: true}, {Text: "not-a-number", IsCorrect: false}}},
			}},
			wantErr: true, errLike: "invalid tolerance value",
		},
		{
			name:  "ES only draft",
			draft: &render.QuizDraft{Title: "Essay Only", ESQuestions: []render.Question{{Number: 1, Text: "Explain."}}},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assessment, err := qti.BuildAssessment(tt.draft)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if assessment != nil {
				if assessment.Assessment.Title == "" {
					t.Fatal("expected non-empty assessment title")
				}
				if len(assessment.Assessment.Sections) == 0 {
					t.Fatal("expected at least one section")
				}
			}
		})
	}
}

func TestMarshal_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		buildAssmt func(t *testing.T) (*qti.Assessment, error)
		wantErr    bool
		errLike    string
		wantToken  []string
	}{
		{
			name: "success",
			buildAssmt: func(t *testing.T) (*qti.Assessment, error) {
				t.Helper()
				return qti.BuildAssessment(sampleDraft())
			},
			wantToken: []string{"<?xml", "Signals Quiz", "questestinterop"},
		},
		{name: "zero assessment", buildAssmt: func(_ *testing.T) (*qti.Assessment, error) { return &qti.Assessment{}, nil }, wantToken: []string{"<?xml"}},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assmt, buildErr := tt.buildAssmt(t)
			if buildErr != nil {
				t.Fatalf("failed to build assessment: %v", buildErr)
			}
			xmlBytes, err := qti.Marshal(assmt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			xmlStr := string(xmlBytes)
			for _, tok := range tt.wantToken {
				if !strings.Contains(xmlStr, tok) {
					t.Fatalf("expected marshaled xml to contain %q", tok)
				}
			}
		})
	}
}
