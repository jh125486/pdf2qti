// Package validate provides quiz question validation.
package validate

import (
	"fmt"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/render"
)

// Result holds validation results.
type Result struct {
	Errors   []string
	Warnings []string
}

// IsValid returns true if there are no errors.
func (r *Result) IsValid() bool {
	return len(r.Errors) == 0
}

// ValidateDraft validates a quiz draft against the given validation config.
func ValidateDraft(d *render.QuizDraft, v config.Validation) *Result {
	result := &Result{}

	// Check sequential numbering
	if v.RequireSequentialNumbering {
		allQs := append(append(d.TFQuestions, d.MAQuestions...), d.MCQuestions...)
		for i, q := range allQs {
			if q.Number != i+1 {
				result.Errors = append(result.Errors, fmt.Sprintf("question %d: expected number %d, got %d", i+1, i+1, q.Number))
			}
		}
	}

	// Validate TF questions
	for _, q := range d.TFQuestions {
		errs := validateTFMC(q, v)
		result.Errors = append(result.Errors, errs...)
	}

	// Validate MC questions
	for _, q := range d.MCQuestions {
		errs := validateTFMC(q, v)
		result.Errors = append(result.Errors, errs...)
	}

	// Validate MA questions
	for _, q := range d.MAQuestions {
		errs := validateMA(q, v)
		result.Errors = append(result.Errors, errs...)
	}

	return result
}

func validateTFMC(q render.Question, v config.Validation) []string {
	var errs []string
	if v.RequireExactlyOneCorrectForTFMC {
		correct := 0
		for _, o := range q.Options {
			if o.IsCorrect {
				correct++
			}
		}
		if correct != 1 {
			errs = append(errs, fmt.Sprintf("question %d: expected exactly 1 correct answer, got %d", q.Number, correct))
		}
	}
	if v.EnforceUniqueOptionsPerQuestion {
		seen := make(map[string]bool)
		for _, o := range q.Options {
			if seen[o.Text] {
				errs = append(errs, fmt.Sprintf("question %d: duplicate option %q", q.Number, o.Text))
			}
			seen[o.Text] = true
		}
	}
	return errs
}

func validateMA(q render.Question, v config.Validation) []string {
	var errs []string
	if v.EnforceUniqueOptionsPerQuestion {
		seen := make(map[string]bool)
		for _, o := range q.Options {
			if seen[o.Text] {
				errs = append(errs, fmt.Sprintf("question %d: duplicate option %q", q.Number, o.Text))
			}
			seen[o.Text] = true
		}
	}
	if v.MAMaxCorrectDensity > 0 && len(q.Options) > 0 {
		correct := 0
		for _, o := range q.Options {
			if o.IsCorrect {
				correct++
			}
		}
		density := float64(correct) / float64(len(q.Options))
		if density > v.MAMaxCorrectDensity {
			errs = append(errs, fmt.Sprintf("question %d: correct density %.2f exceeds max %.2f", q.Number, density, v.MAMaxCorrectDensity))
		}
	}
	return errs
}
