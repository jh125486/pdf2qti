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
