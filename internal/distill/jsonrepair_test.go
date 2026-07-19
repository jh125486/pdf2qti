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
		{name: "valid escapes untouched", in: `{"a":"line1\nline2\t\"q\""}`, want: `{"a":"line1\nline2\t\"q\""}`},
		{name: "latex inline math doubled", in: `{"a":"\(x^2\)"}`, want: `{"a":"\\(x^2\\)"}`},
		{name: "latex command doubled", in: `{"a":"\alpha"}`, want: `{"a":"\\alpha"}`},
		{name: "backslash-t left alone (\\t is a valid JSON escape)", in: `{"a":"\text"}`, want: `{"a":"\text"}`},
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
