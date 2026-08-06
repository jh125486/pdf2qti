// Whitebox package: mathWarnings, add, and warnings are unexported, and a nil *mathWarnings
// (never produced by Render itself, which always passes a real &mathWarnings{}) can only be
// constructed and exercised from inside the package.
package pptx

import (
	"errors"
	"strings"
	"testing"
)

func TestMathWarnings_AddAndWarnings(t *testing.T) {
	t.Parallel()

	errUnconvertible := errors.New("no <m:oMath> in pandoc output")

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "nil receiver is safe to call add on",
			run: func(t *testing.T) {
				t.Helper()
				var w *mathWarnings
				w.add("x^2", errUnconvertible) // must not panic
				if got := w.warnings(); got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			},
		},
		{
			name: "nil receiver returns nil warnings",
			run: func(t *testing.T) {
				t.Helper()
				var w *mathWarnings
				if got := w.warnings(); got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			},
		},
		{
			name: "zero value is ready to use without a constructor",
			run: func(t *testing.T) {
				t.Helper()
				w := &mathWarnings{}
				w.add("x^2", errUnconvertible)
				got := w.warnings()
				if len(got) != 1 {
					t.Fatalf("got %d warnings, want 1: %v", len(got), got)
				}
				if !strings.Contains(got[0], `"x^2"`) {
					t.Fatalf("warning %q missing %q", got[0], `"x^2"`)
				}
			},
		},
		{
			name: "duplicate formula is deduped, not reported twice",
			run: func(t *testing.T) {
				t.Helper()
				w := &mathWarnings{}
				w.add("x^2", errUnconvertible)
				w.add("x^2", errUnconvertible)
				if got := w.warnings(); len(got) != 1 {
					t.Fatalf("got %d warnings, want 1 (deduped): %v", len(got), got)
				}
			},
		},
		{
			name: "different formulas are both reported, in call order",
			run: func(t *testing.T) {
				t.Helper()
				w := &mathWarnings{}
				w.add("x^2", errUnconvertible)
				w.add("y^3", errUnconvertible)
				got := w.warnings()
				if len(got) != 2 {
					t.Fatalf("got %d warnings, want 2: %v", len(got), got)
				}
				if !strings.Contains(got[0], `"x^2"`) || !strings.Contains(got[1], `"y^3"`) {
					t.Fatalf("warnings not in call order: %v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}
