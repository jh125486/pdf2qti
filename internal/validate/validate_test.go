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

func TestValidateDraft_SequentialNumbering(t *testing.T) {
	tests := []struct {
		name      string
		draft     *render.QuizDraft
		wantValid bool
	}{
		{
			name: "valid sequential",
			draft: makeDraft(
				[]render.Question{{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False"}}}},
				nil,
				[]render.Question{{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B"}}}},
			),
			wantValid: true,
		},
		{
			name: "invalid sequential",
			draft: makeDraft(
				[]render.Question{{Number: 5, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}}}},
				nil,
				nil,
			),
			wantValid: false,
		},
	}

	v := config.Validation{RequireSequentialNumbering: true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validate.ValidateDraft(tt.draft, v)
			if result.IsValid() != tt.wantValid {
				t.Errorf("IsValid()=%v, want %v; errors: %v", result.IsValid(), tt.wantValid, result.Errors)
			}
		})
	}
}

func TestValidateDraft_TFExactlyOneCorrect(t *testing.T) {
	v := config.Validation{RequireExactlyOneCorrectForTFMC: true}

	t.Run("zero correct", func(t *testing.T) {
		draft := makeDraft(
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "True", IsCorrect: false},
				{Text: "False", IsCorrect: false},
			}}},
			nil, nil,
		)
		result := validate.ValidateDraft(draft, v)
		if result.IsValid() {
			t.Error("expected invalid")
		}
	})

	t.Run("two correct", func(t *testing.T) {
		draft := makeDraft(
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: true},
			}}},
			nil, nil,
		)
		result := validate.ValidateDraft(draft, v)
		if result.IsValid() {
			t.Error("expected invalid")
		}
	})

	t.Run("exactly one correct", func(t *testing.T) {
		draft := makeDraft(
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: false},
			}}},
			nil, nil,
		)
		result := validate.ValidateDraft(draft, v)
		if !result.IsValid() {
			t.Errorf("expected valid; errors: %v", result.Errors)
		}
	})
}

func TestValidateDraft_MADensity(t *testing.T) {
	v := config.Validation{MAMaxCorrectDensity: 0.5}

	t.Run("density exceeded", func(t *testing.T) {
		// 3 correct out of 4 = 0.75, exceeds 0.5
		draft := makeDraft(nil,
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "A", IsCorrect: true},
				{Text: "B", IsCorrect: true},
				{Text: "C", IsCorrect: true},
				{Text: "D", IsCorrect: false},
			}}},
			nil,
		)
		result := validate.ValidateDraft(draft, v)
		if result.IsValid() {
			t.Error("expected invalid due to density")
		}
	})

	t.Run("density ok", func(t *testing.T) {
		// 1 correct out of 4 = 0.25, within 0.5
		draft := makeDraft(nil,
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "A", IsCorrect: true},
				{Text: "B", IsCorrect: false},
				{Text: "C", IsCorrect: false},
				{Text: "D", IsCorrect: false},
			}}},
			nil,
		)
		result := validate.ValidateDraft(draft, v)
		if !result.IsValid() {
			t.Errorf("expected valid; errors: %v", result.Errors)
		}
	})
}

func TestValidateDraft_DuplicateOptions(t *testing.T) {
	v := config.Validation{EnforceUniqueOptionsPerQuestion: true}

	t.Run("duplicate MC options", func(t *testing.T) {
		draft := makeDraft(nil, nil,
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "A", IsCorrect: true},
				{Text: "A", IsCorrect: false},
			}}},
		)
		result := validate.ValidateDraft(draft, v)
		if result.IsValid() {
			t.Error("expected invalid due to duplicate options")
		}
	})

	t.Run("duplicate MA options", func(t *testing.T) {
		draft := makeDraft(nil,
			[]render.Question{{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "Same", IsCorrect: true},
				{Text: "Same", IsCorrect: false},
			}}},
			nil,
		)
		result := validate.ValidateDraft(draft, v)
		if result.IsValid() {
			t.Error("expected invalid due to duplicate MA options")
		}
	})
}
