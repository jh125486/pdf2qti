package validate_test

import (
	"strings"
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
		wantError string
	}{
		{name: "TFQuestions rejects source references", draft: &render.QuizDraft{TFQuestions: []render.Question{{Number: 3, Text: "According to the source, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "MAQuestions rejects source references", draft: &render.QuizDraft{MAQuestions: []render.Question{{Number: 3, Text: "According to THE TEXT, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "MCQuestions rejects source references", draft: &render.QuizDraft{MCQuestions: []render.Question{{Number: 3, Text: "According to the chapter, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "SAQuestions rejects source references", draft: &render.QuizDraft{SAQuestions: []render.Question{{Number: 3, Text: "According to the passage, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "ESQuestions rejects source references", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 3, Text: "According to the author, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "MTQuestions rejects source references", draft: &render.QuizDraft{MTQuestions: []render.Question{{Number: 3, Text: "According to the source, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "NRQuestions rejects source references", draft: &render.QuizDraft{NRQuestions: []render.Question{{Number: 3, Text: "According to the text, what happens?"}}}, wantError: "question 3: forbidden phrase"},
		{name: "option reference rejected", draft: &render.QuizDraft{MCQuestions: []render.Question{{Number: 4, Text: "What happens?", Options: []render.Option{{Text: "As the author explains"}}}}}, wantError: "question 4 option 1: forbidden phrase"},
		{
			name:      "TF unused empty matching text ignored",
			draft:     &render.QuizDraft{TFQuestions: []render.Question{{Number: 1, Text: "Trees have nodes.", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False"}}}}},
			v:         config.Validation{ForbiddenPhrases: []string{`^$`}},
			wantValid: true,
		},
		{
			name:      "MC unused empty matching text ignored",
			draft:     &render.QuizDraft{MCQuestions: []render.Question{{Number: 2, Text: "How many children can a binary tree node have?", Options: []render.Option{{Text: "Two", IsCorrect: true}, {Text: "Three"}}}}},
			v:         config.Validation{ForbiddenPhrases: []string{`^$`}},
			wantValid: true,
		},
		{
			name:      "MT empty matching answer checked",
			draft:     &render.QuizDraft{MTQuestions: []render.Question{{Number: 3, Text: "Match tree concepts.", Options: []render.Option{{Text: "Root", MatchText: ""}}}}},
			v:         config.Validation{ForbiddenPhrases: []string{`^$`}},
			wantError: "question 3 option 1 matching answer: forbidden phrase",
		},
		{name: "matching answer reference rejected", draft: &render.QuizDraft{MTQuestions: []render.Question{{Number: 5, Text: "Match concepts", Options: []render.Option{{Text: "Concept", MatchText: "the passage"}}}}}, wantError: "question 5 option 1 matching answer: forbidden phrase"},
		{name: "custom regex replaces builtins", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 1, Text: "the source"}}}, v: config.Validation{ForbiddenPhrases: []string{`(?i)forbidden`}}, wantValid: true},
		{name: "custom regex matched", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 2, Text: "FORBIDDEN content"}}}, v: config.Validation{ForbiddenPhrases: []string{`(?i)forbidden`}}, wantError: "question 2: forbidden phrase"},
		{name: "empty list disables rules", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 1, Text: "the source"}}}, v: config.Validation{ForbiddenPhrases: []string{}}, wantValid: true},
		{name: "word boundaries prevent partial matches", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 1, Text: "Describe the sourcebook and the textbook."}}}, wantValid: true},
		{name: "malformed programmatic rule fails", draft: makeDraft(nil, nil, nil), v: config.Validation{ForbiddenPhrases: []string{"["}}, wantError: "invalid regular expression"},
		{name: "empty matching rule applies", draft: &render.QuizDraft{ESQuestions: []render.Question{{Number: 1, Text: "Question"}}}, v: config.Validation{ForbiddenPhrases: []string{"^"}}, wantError: "question 1: forbidden phrase"},
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
			if tt.wantError != "" && !strings.Contains(strings.Join(result.Errors, "\n"), tt.wantError) {
				t.Errorf("errors=%v want substring %q", result.Errors, tt.wantError)
			}
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
