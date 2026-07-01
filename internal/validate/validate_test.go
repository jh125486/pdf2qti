package validate_test

import (
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/render"
	"github.com/jh125486/pdf2qti/internal/validate"
)

func makeDraft(tf, ma, mc []render.Question) *render.QuizDraft {
	return &render.QuizDraft{
		Title:       "Test",
		TFQuestions: tf,
		MAQuestions: ma,
		MCQuestions: mc,
	}
}

func TestValidateDraft_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		draft     *render.QuizDraft
		v         config.Validation
		wantValid bool
	}{
		{
			name: "valid sequential numbering",
			draft: makeDraft(
				[]render.Question{{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False"}}}},
				nil,
				[]render.Question{{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B"}}}},
			),
			v:         config.Validation{RequireSequentialNumbering: true},
			wantValid: true,
		},
		{
			name: "invalid sequential numbering",
			draft: makeDraft(
				[]render.Question{{Number: 5, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}}}},
				nil, nil,
			),
			v:         config.Validation{RequireSequentialNumbering: true},
			wantValid: false,
		},
		{
			name: "TF zero correct options",
			draft: makeDraft(
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "True", IsCorrect: false}, {Text: "False", IsCorrect: false},
				}}},
				nil, nil,
			),
			v:         config.Validation{RequireExactlyOneCorrectForTFMC: true},
			wantValid: false,
		},
		{
			name: "TF two correct options",
			draft: makeDraft(
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: true},
				}}},
				nil, nil,
			),
			v:         config.Validation{RequireExactlyOneCorrectForTFMC: true},
			wantValid: false,
		},
		{
			name: "TF exactly one correct option",
			draft: makeDraft(
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false},
				}}},
				nil, nil,
			),
			v:         config.Validation{RequireExactlyOneCorrectForTFMC: true},
			wantValid: true,
		},
		{
			name: "MC exactly one correct option",
			draft: makeDraft(nil, nil,
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false},
				}}},
			),
			v:         config.Validation{RequireExactlyOneCorrectForTFMC: true},
			wantValid: true,
		},
		{
			name: "MA density exceeded",
			draft: makeDraft(nil,
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: true},
					{Text: "C", IsCorrect: true}, {Text: "D", IsCorrect: false},
				}}},
				nil,
			),
			v:         config.Validation{MAMaxCorrectDensity: 0.5},
			wantValid: false,
		},
		{
			name: "MA density within limit",
			draft: makeDraft(nil,
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false},
					{Text: "C", IsCorrect: false}, {Text: "D", IsCorrect: false},
				}}},
				nil,
			),
			v:         config.Validation{MAMaxCorrectDensity: 0.5},
			wantValid: true,
		},
		{
			name: "MC duplicate options",
			draft: makeDraft(nil, nil,
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "A", IsCorrect: false},
				}}},
			),
			v:         config.Validation{EnforceUniqueOptionsPerQuestion: true},
			wantValid: false,
		},
		{
			name: "MA duplicate options",
			draft: makeDraft(nil,
				[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
					{Text: "Same", IsCorrect: true}, {Text: "Same", IsCorrect: false},
				}}},
				nil,
			),
			v:         config.Validation{EnforceUniqueOptionsPerQuestion: true},
			wantValid: false,
		},
		{
			name:      "empty draft with no rules",
			draft:     makeDraft(nil, nil, nil),
			v:         config.Validation{},
			wantValid: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := validate.ValidateDraft(tt.draft, tt.v)
			if result.IsValid() != tt.wantValid {
				t.Errorf("IsValid()=%v, want %v; errors: %v", result.IsValid(), tt.wantValid, result.Errors)
			}
		})
	}
}

func TestIsValid_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *validate.Result
		want   bool
	}{
		{name: "no errors", result: &validate.Result{}, want: true},
		{name: "with warnings only", result: &validate.Result{Warnings: []string{"careful"}}, want: true},
		{name: "with errors", result: &validate.Result{Errors: []string{"bad"}}, want: false},
		{name: "with errors and warnings", result: &validate.Result{Errors: []string{"bad"}, Warnings: []string{"careful"}}, want: false},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.IsValid(); got != tt.want {
				t.Errorf("IsValid()=%v, want %v", got, tt.want)
			}
		})
	}
}
