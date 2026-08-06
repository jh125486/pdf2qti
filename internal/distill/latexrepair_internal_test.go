// Whitebox package: repairMatrixRowSeparators is unexported.
package distill

import "testing"

func TestRepairMatrixRowSeparators_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no matrix, unchanged",
			in:   `Just a plain bullet, no LaTeX at all.`,
			want: `Just a plain bullet, no LaTeX at all.`,
		},
		{
			name: "bare row separator inside bmatrix is doubled",
			in:   `\(\mathbf{u} = \begin{bmatrix} u_1 + v_1 \ u_2 + v_2 \end{bmatrix}\)`,
			want: `\(\mathbf{u} = \begin{bmatrix} u_1 + v_1 \\ u_2 + v_2 \end{bmatrix}\)`,
		},
		{
			name: "already-doubled row separator is left unchanged",
			in:   `\(\mathbf{v} = \begin{bmatrix} -4 \\ 3 \end{bmatrix}\)`,
			want: `\(\mathbf{v} = \begin{bmatrix} -4 \\ 3 \end{bmatrix}\)`,
		},
		{
			// The false-positive-avoidance case: "\ " (backslash-space) is a legitimate LaTeX
			// explicit-space command outside a matrix environment — doubling it there would
			// change its meaning, so repairMatrixRowSeparators must never touch it.
			name: "bare backslash-space outside any matrix environment is left unchanged",
			in:   `Some text with an explicit LaTeX space \ here, no matrix involved.`,
			want: `Some text with an explicit LaTeX space \ here, no matrix involved.`,
		},
		{
			name: "multiple matrices in one bullet, only the bare one is fixed",
			in:   `Compare \begin{bmatrix} a \ b \end{bmatrix} to \begin{bmatrix} c \\ d \end{bmatrix}.`,
			want: `Compare \begin{bmatrix} a \\ b \end{bmatrix} to \begin{bmatrix} c \\ d \end{bmatrix}.`,
		},
		{
			name: "pmatrix environment variant is also fixed",
			in:   `\begin{pmatrix} 1 \ 2 \end{pmatrix}`,
			want: `\begin{pmatrix} 1 \\ 2 \end{pmatrix}`,
		},
		{
			name: "cases environment is also fixed",
			in:   `f(x) = \begin{cases} 1 \ -1 \end{cases}`,
			want: `f(x) = \begin{cases} 1 \\ -1 \end{cases}`,
		},
		{
			// The ch05/ch09/ch10 regression: compact matrix notation with no whitespace around the
			// row separator at all. Caught by comparing math-span counts in generated slide
			// Markdown against actual <m:oMath> elements in the rendered PPTX (see
			// repairMatrixRowSeparators's doc comment) -- pandoc silently failed to convert every
			// one of these to OOXML math, since a bare "\" directly followed by a digit or letter
			// isn't valid LaTeX syntax at all.
			name: "compact bare separator directly abutting a single-digit cell is doubled",
			in:   `\begin{bmatrix}1&0\0&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\0&-1\end{bmatrix}`,
		},
		{
			name: "compact bare separator abutting a negative single-digit cell is doubled",
			in:   `\begin{bmatrix}1&0\-1&0\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\-1&0\end{bmatrix}`,
		},
		{
			name: "compact bare separator abutting a single-letter variable cell is doubled",
			in:   `\begin{bmatrix}1&k\0&1\end{bmatrix}`,
			want: `\begin{bmatrix}1&k\\0&1\end{bmatrix}`,
		},
		{
			name: "compact bare separator abutting a negative single-letter variable cell is doubled",
			in:   `\begin{bmatrix}1&0\-k&1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\-k&1\end{bmatrix}`,
		},
		{
			name: "compact bare separator directly before end (last row, single-digit cell) is doubled",
			in:   `\begin{bmatrix}0&1\1&0\end{bmatrix}`,
			want: `\begin{bmatrix}0&1\\1&0\end{bmatrix}`,
		},
		{
			name: "compact bare separator abutting a multi-digit cell is doubled",
			in:   `\begin{bmatrix}1&0\12&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\12&-1\end{bmatrix}`,
		},
		{
			// The false-positive-avoidance case for the new compact-notation path: a genuine
			// multi-letter LaTeX command (theta, a real Greek-letter macro) used as a matrix cell's
			// entire content, directly abutting the next cell with no whitespace, must NOT be
			// mistaken for a malformed row separator plus a one-letter cell "t" -- doubling it
			// would turn the correct "\theta" into the wrong "\\theta" (an escaped literal
			// backslash followed by unrelated text "theta", not the theta symbol).
			name: "compact single-backslash multi-letter command as a cell is left unchanged",
			in:   `\begin{bmatrix}\theta&\theta\end{bmatrix}`,
			want: `\begin{bmatrix}\theta&\theta\end{bmatrix}`,
		},
		{
			// Already-correct doubled separator immediately followed by a lone-cell-shaped token
			// must not be corrupted into three backslashes by the new lone-cell path matching the
			// separator's second backslash.
			name: "already-doubled separator abutting a lone-cell token is left unchanged",
			in:   `\begin{bmatrix}1&0\\0&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\0&-1\end{bmatrix}`,
		},
		{
			// array is the standard way to typeset an augmented matrix with a vertical divider
			// (e.g. Gaussian elimination), which none of the matrix variants support at all.
			// array's own "{cc|c}" column-spec argument right after "\begin{array}" is inert text
			// (no backslash) and must survive untouched alongside the separator fix.
			name: "array environment (augmented matrix with a divider) is fixed",
			in:   `\begin{array}{cc|c} 1 & 0 & 2 \ 0 & 1 & 3 \end{array}`,
			want: `\begin{array}{cc|c} 1 & 0 & 2 \\ 0 & 1 & 3 \end{array}`,
		},
		{
			// aligned nests inside "\(...\)" for a multi-step derivation, using the identical
			// bare-vs-doubled row-separator convention as the matrix variants.
			name: "aligned environment is fixed",
			in:   `\(\begin{aligned} x &= 1 \ y &= 2 \end{aligned}\)`,
			want: `\(\begin{aligned} x &= 1 \\ y &= 2 \end{aligned}\)`,
		},
		{
			name: "gathered environment is fixed",
			in:   `\(\begin{gathered} x = 1 \ y = 2 \end{gathered}\)`,
			want: `\(\begin{gathered} x = 1 \\ y = 2 \end{gathered}\)`,
		},
		{
			// align (and align*) are less likely to arrive wrapped in "\(...\)"/"\[...\]" in
			// practice (they're normally standalone, unlike aligned/gathered), but fixing their
			// row separators is still correct behavior regardless of how they're delimited.
			name: "align environment is fixed",
			in:   `\begin{align} x &= 1 \ y &= 2 \end{align}`,
			want: `\begin{align} x &= 1 \\ y &= 2 \end{align}`,
		},
		{
			name: "align* environment is fixed",
			in:   `\begin{align*} x &= 1 \ y &= 2 \end{align*}`,
			want: `\begin{align*} x &= 1 \\ y &= 2 \end{align*}`,
		},
		{
			// The compact-notation gap for a spacing arg instead of a cell value: a bare
			// separator directly abutting "[6pt]" (LaTeX's own syntax for extra vertical space
			// after a row break) with no whitespace at all.
			name: "compact bare separator directly abutting a spacing arg is doubled",
			in:   `\begin{bmatrix}1&0\[6pt]0&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\[6pt]0&-1\end{bmatrix}`,
		},
		{
			name: "compact bare separator abutting a decimal-length spacing arg is doubled",
			in:   `\begin{bmatrix}1&0\[.5em]0&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\\[.5em]0&-1\end{bmatrix}`,
		},
		{
			// A "[" not shaped like a plausible spacing arg (no closing "]" within a short
			// distance) must not be mistaken for one.
			name: "bare backslash before an unrelated bracketed span is left unchanged",
			in:   `\begin{bmatrix}1&0\[this is not a spacing arg at all]0&-1\end{bmatrix}`,
			want: `\begin{bmatrix}1&0\[this is not a spacing arg at all]0&-1\end{bmatrix}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := repairMatrixRowSeparators(tt.in)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepairNulBytes_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no NUL byte, unchanged",
			in:   `Just a plain bullet, no LaTeX at all.`,
			want: `Just a plain bullet, no LaTeX at all.`,
		},
		{
			// Observed in practice against the real OpenAI API (gpt-5.6-luna, gpt-5.6-terra): a
			// NUL byte appears exactly where a LaTeX command's leading backslash should be, with
			// the command's letters intact right after.
			name: "NUL before a LaTeX command is replaced with a backslash",
			in:   nulByte + "mathbf{v}",
			want: `\mathbf{v}`,
		},
		{
			name: "NUL on both sides of a math delimiter is replaced with backslashes",
			in:   "The origin is " + nulByte + "(0,0" + nulByte + ") in two dimensions.",
			want: `The origin is \(0,0\) in two dimensions.`,
		},
		{
			name: "multiple NUL bytes across a bullet are all replaced",
			in:   nulByte + "mathbb{R}^n uses " + nulByte + "text{n-tuples}.",
			want: `\mathbb{R}^n uses \text{n-tuples}.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := repairNulBytes(tt.in)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
