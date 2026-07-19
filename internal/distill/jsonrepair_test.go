// Whitebox package: repairJSONEscapes is unexported.
package distill

import (
	"encoding/json"
	"testing"
)

func TestRepairJSONEscapes_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no backslashes", in: `{"a":"b"}`, want: `{"a":"b"}`},
		{name: "valid escapes untouched", in: `{"a":"line1\nline2\"q\""}`, want: `{"a":"line1\nline2\"q\""}`},
		{name: "latex inline math doubled", in: `{"a":"\(x^2\)"}`, want: `{"a":"\\(x^2\\)"}`},
		{name: "latex command doubled", in: `{"a":"\alpha"}`, want: `{"a":"\\alpha"}`},
		{name: "backslash-t doubled, not left as tab", in: `{"a":"\text"}`, want: `{"a":"\\text"}`},
		{name: "backslash-r doubled, not left as carriage return", in: `{"a":"\rangle"}`, want: `{"a":"\\rangle"}`},
		{name: "backslash-b doubled, not left as backspace", in: `{"a":"\begin{bmatrix}"}`, want: `{"a":"\\begin{bmatrix}"}`},
		{name: "backslash-f doubled, not left as form feed", in: `{"a":"\frac{1}{2}"}`, want: `{"a":"\\frac{1}{2}"}`},
		{name: "trailing backslash", in: `{"a":"foo\`, want: `{"a":"foo\\`},
		{
			// Regression: a valid "\\" escape immediately followed by an invalid "\(" used to
			// leave the second backslash of "\\" re-examined as its own (invalid) escape start,
			// corrupting otherwise-valid input. Common in LaTeX matrix content ("\\" row breaks
			// next to "\(" inline math).
			name: "escaped backslash directly followed by invalid escape",
			in:   `{"a":"\\\(x\)"}`,
			want: `{"a":"\\\\(x\\)"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := repairJSONEscapes(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepairJSONEscapes_MakesInvalidJSONValid(t *testing.T) {
	t.Parallel()

	raw := `{"text":"the formula \(x^2 + y^2 = z^2\) is Pythagorean"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON still invalid: %v (repaired=%q)", err, repaired)
	}
	want := `the formula \(x^2 + y^2 = z^2\) is Pythagorean`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

func TestRepairJSONEscapes_LatexBeginNotBackspace(t *testing.T) {
	t.Parallel()

	// \b is technically a valid JSON escape (backspace, 0x08). Before this fix, a model
	// forgetting to double-escape "\begin{bmatrix}" produced JSON that parsed "successfully" —
	// but into a literal backspace byte + "egin{bmatrix}", silently corrupting the LaTeX rather
	// than erroring, since backspace-then-text is syntactically valid JSON.
	raw := `{"text":"solve \begin{bmatrix}1&0\end{bmatrix}"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON invalid: %v (repaired=%q)", err, repaired)
	}
	want := `solve \begin{bmatrix}1&0\end{bmatrix}`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

func TestRepairJSONEscapes_LatexTimesNotTab(t *testing.T) {
	t.Parallel()

	// \t is technically a valid JSON escape (tab, 0x09). Before this fix, a model forgetting to
	// double-escape "n \times m" produced JSON that parsed "successfully" — but into "n" + a tab
	// byte + "imes m", which renders as "nimesm" once the tab collapses/disappears downstream.
	// Many common LaTeX commands start with a "t" ("\times", "\tan", "\theta", "\tau", "\text").
	raw := `{"text":"verify the n \times m dimensions"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON invalid: %v (repaired=%q)", err, repaired)
	}
	want := `verify the n \times m dimensions`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

func TestRepairJSONEscapes_AdjacentValidAndInvalidEscapes(t *testing.T) {
	t.Parallel()

	// LaTeX matrix row-break "\\" directly adjacent to an unescaped "\(" — see regression note
	// in TestRepairJSONEscapes_Table.
	raw := `{"warning":"row break \\ then \(x\) differs from earlier"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON still invalid: %v (repaired=%q)", err, repaired)
	}
}
