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
		{name: "empty string", in: ``, want: ``},
		{name: "no backslashes", in: `{"a":"b"}`, want: `{"a":"b"}`},
		{name: "preserved quote escape only", in: `{"a":"he said \"hi\""}`, want: `{"a":"he said \"hi\""}`},
		{name: "preserved slash escape", in: `{"a":"a\/b"}`, want: `{"a":"a\/b"}`},
		{name: "backslash-n doubled, not newline (latex nabla)", in: `{"a":"\nabla"}`, want: `{"a":"\\nabla"}`},
		{name: "genuine newline escape now doubled (domain choice)", in: `{"a":"line1\nline2"}`, want: `{"a":"line1\\nline2"}`},
		{
			name: "combined preserved-quote and doubled backslash-n",
			in:   `{"a":"line1\nline2\"q\""}`,
			want: `{"a":"line1\\nline2\"q\""}`,
		},
		{name: "backslash-neq doubled", in: `{"a":"a \neq b"}`, want: `{"a":"a \\neq b"}`},
		{name: "backslash-notin doubled", in: `{"a":"x \notin S"}`, want: `{"a":"x \\notin S"}`},
		{name: "backslash-newcommand doubled", in: `{"a":"\newcommand"}`, want: `{"a":"\\newcommand"}`},
		{name: "backslash-t doubled, not tab", in: `{"a":"\text"}`, want: `{"a":"\\text"}`},
		{name: "backslash-r doubled, not carriage return", in: `{"a":"\rangle"}`, want: `{"a":"\\rangle"}`},
		{name: "backslash-b doubled, not backspace", in: `{"a":"\begin{bmatrix}"}`, want: `{"a":"\\begin{bmatrix}"}`},
		{name: "backslash-f doubled, not form feed", in: `{"a":"\frac{1}{2}"}`, want: `{"a":"\\frac{1}{2}"}`},
		{name: "latex inline math punctuation doubled", in: `{"a":"\(x^2\)"}`, want: `{"a":"\\(x^2\\)"}`},
		{name: "latex generic command doubled", in: `{"a":"\alpha"}`, want: `{"a":"\\alpha"}`},
		{name: "backslash-percent doubled", in: `{"a":"50\%"}`, want: `{"a":"50\\%"}`},
		{name: "valid unicode escape kept, all digits", in: `{"a":"\u2260"}`, want: `{"a":"\u2260"}`},
		{name: "valid unicode escape kept, lowercase hex", in: `{"a":"\u00ab"}`, want: `{"a":"\u00ab"}`},
		{name: "valid unicode escape kept, uppercase hex", in: `{"a":"\u00AB"}`, want: `{"a":"\u00AB"}`},
		{name: "backslash-u latex, non-hex first char (underline)", in: `{"a":"\underline"}`, want: `{"a":"\\underline"}`},
		{name: "backslash-u latex, non-hex first char (uparrow)", in: `{"a":"\uparrow"}`, want: `{"a":"\\uparrow"}`},
		{name: "backslash-u with 3 hex then non-hex 4th char doubled", in: `\u123g`, want: `\\u123g`},
		{name: "backslash-u truncated, fewer than 4 trailing chars", in: `\u12`, want: `\\u12`},
		{name: "backslash-u at end of string, nothing after", in: `\u`, want: `\\u`},
		{name: "trailing truncated backslash at end of string", in: `{"a":"foo\`, want: `{"a":"foo\\`},
		{
			// Regression: a raw "\\" (LaTeX row break) directly followed by an invalid "\("
			// used to be treated as an already-escaped single backslash and left untouched,
			// silently dropping a backslash. All three raw backslashes here (row break "\\"
			// plus inline-math-open "\(") must round-trip intact: doubled independently.
			name: "raw double-backslash directly followed by invalid escape",
			in:   `{"a":"\\\(x\)"}`,
			want: `{"a":"\\\\\\(x\\)"}`,
		},
		{name: "multibyte rune immediately after backslash", in: "\\€", want: "\\\\€"},
		{name: "multibyte rune elsewhere, latex command intact", in: `{"a":"\alpha€"}`, want: `{"a":"\\alpha€"}`},
		{name: "backslash-u immediately followed by multibyte rune", in: "\\u€", want: "\\\\u€"},
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

func TestRepairJSONEscapes_LatexNablaNotNewline(t *testing.T) {
	t.Parallel()

	// \n is technically a valid JSON escape (newline, 0x0A). Before this fix, a model
	// forgetting to double-escape "\nabla f" produced JSON that parsed "successfully" — but
	// into a literal newline byte + "abla f", silently corrupting the LaTeX. Many common LaTeX
	// commands start with "n" ("\nabla", "\neq", "\notin", "\newcommand", "\nonumber").
	raw := `{"text":"gradient \nabla f is zero"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON invalid: %v (repaired=%q)", err, repaired)
	}
	want := `gradient \nabla f is zero`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

func TestRepairJSONEscapes_LatexUnderlineNotUnicode(t *testing.T) {
	t.Parallel()

	// \u is the start of a valid JSON unicode escape (\uXXXX) but also the start of several
	// LaTeX commands ("\underline", "\uparrow", "\uplus"). Before this fix, "u" was whitelisted
	// unconditionally without checking for 4 trailing hex digits, so "\underline" corrupted
	// json.Unmarshal into an "invalid \u escape" hard failure instead of being doubled.
	raw := `{"text":"\underline{x}"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON invalid: %v (repaired=%q)", err, repaired)
	}
	want := `\underline{x}`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

func TestRepairJSONEscapes_GenuineUnicodeEscapePreserved(t *testing.T) {
	t.Parallel()

	// A genuine \uXXXX unicode escape (4 valid hex digits) must be preserved, not doubled, so
	// that json.Unmarshal decodes it to the intended non-ASCII character.
	raw := `{"text":"a \u2260 b"}`
	repaired := repairJSONEscapes(raw)

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(repaired), &out); err != nil {
		t.Fatalf("repaired JSON invalid: %v (repaired=%q)", err, repaired)
	}
	want := "a \u2260 b"
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

// TestUnmarshalRepaired_AlreadyValidEscapeNotOverDoubled is the regression test for the
// ch01_slides.md slide-3 bug: a model response containing "\\overrightarrow" (a genuine, already
// -correct JSON escape for a single literal backslash immediately followed by an unrelated LaTeX
// command) parses as valid JSON on its own. repairJSONEscapes can't tell that "\\" apart from two
// independent raw backslashes each needing doubling, and mangles it into two literal backslashes
// if run unconditionally. unmarshalRepaired must only reach for repairJSONEscapes when the raw
// response fails to parse as-is, so this case is decoded correctly without ever invoking it.
func TestUnmarshalRepaired_AlreadyValidEscapeNotOverDoubled(t *testing.T) {
	t.Parallel()

	raw := `{"text":"directed line segment \\(\\overrightarrow{AB}\\)"}`
	var out struct {
		Text string `json:"text"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := `directed line segment \(\overrightarrow{AB}\)`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

// TestUnmarshalRepaired_FallsBackOnBrokenRawJSON confirms unmarshalRepaired still repairs
// genuinely broken input (raw, unescaped LaTeX backslashes that fail to parse as JSON on their
// own) exactly as repairJSONEscapes always has — the fallback path, not just the fast path.
func TestUnmarshalRepaired_FallsBackOnBrokenRawJSON(t *testing.T) {
	t.Parallel()

	raw := `{"text":"\overrightarrow{AB}"}`
	var out struct {
		Text string `json:"text"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := `\overrightarrow{AB}`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}
