// Whitebox package: repairJSONEscapes is unexported.
package distill

import (
	"encoding/json"
	"reflect"
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

// TestUnmarshalRepaired_Table covers unmarshalRepaired's paths: raw input that's already valid
// JSON with no ambiguous control escape must be decoded as-is without ever reaching
// repairJSONEscapes (the regression case for the ch01_slides.md slide-3 bug — a model response
// containing "\\overrightarrow", a genuine, already-correct JSON escape for a single literal
// backslash immediately followed by an unrelated LaTeX command, that repairJSONEscapes can't tell
// apart from two independent raw backslashes each needing doubling, and would mangle into two
// literal backslashes if run unconditionally); raw input that fails to parse must fall back to
// repairJSONEscapes; and — critically — raw input containing an ambiguous control escape
// (b/f/n/r/t) must ALSO always fall back to repairJSONEscapes even though it parses successfully
// on its own, since trusting that parse is exactly the bug class repairJSONEscapes exists to
// catch (\nabla/\times/\begin/\frac/\right decoding into a control byte plus mangled text).
func TestUnmarshalRepaired_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "already-valid escape decoded as-is, not over-doubled",
			raw:  `{"text":"directed line segment \\(\\overrightarrow{AB}\\)"}`,
			want: `directed line segment \(\overrightarrow{AB}\)`,
		},
		{
			name: "broken raw JSON falls back to repairJSONEscapes",
			raw:  `{"text":"\overrightarrow{AB}"}`,
			want: `\overrightarrow{AB}`,
		},
		{
			name: "ambiguous control escape (nabla) repaired despite parsing successfully as-is",
			raw:  `{"text":"gradient \nabla f is zero"}`,
			want: `gradient \nabla f is zero`,
		},
		{
			name: "ambiguous control escape (times) repaired despite parsing successfully as-is",
			raw:  `{"text":"3 \times 4"}`,
			want: `3 \times 4`,
		},
		{
			name: "ambiguous control escape (begin) repaired despite parsing successfully as-is",
			raw:  `{"text":"\begin{bmatrix} 1 \end{bmatrix}"}`,
			want: `\begin{bmatrix} 1 \end{bmatrix}`,
		},
		{
			// Observed in practice against the real OpenAI API: a model occasionally prepends a
			// preamble sentence to a JSON response despite being asked for "only JSON," making
			// the whole response fail to parse (a distinct failure class from backslash
			// escaping — see distill.go and outline_sections.go's callers, which all route
			// through this function and hit this in production during expand-batch calls).
			name: "leading prose before the JSON object is trimmed",
			raw:  `Here is the requested JSON: {"text":"hello"}`,
			want: `hello`,
		},
		{
			name: "trailing prose after the JSON object is ignored",
			raw:  `{"text":"hello"} Let me know if you need anything else!`,
			want: `hello`,
		},
		{
			// This package's prompts embed chapter source text full of LaTeX braces
			// ("\begin{bmatrix}..."), so a preamble sentence quoting that source can contain a
			// "decoy" '{' well before the real JSON object starts. Trimming to just the first
			// '{' (a simpler fix that was tried and didn't hold up against the real API) would
			// start parsing mid-LaTeX and fail; every '{' must be tried in turn.
			name: "decoy brace from quoted LaTeX source before the real JSON object",
			raw:  `Sure, using \begin{bmatrix} 1 & 2 \end{bmatrix} as reference: {"text":"hello"}`,
			want: `hello`,
		},
		{
			// Caught in PR review: a leading example/illustration of the requested shape is a
			// COMPLETE, validly-decodable JSON object in its own right (unlike a LaTeX decoy
			// brace, which never is) — decodeJSON must not stop at the first successful decode
			// and silently keep this wrong-but-valid one; it must keep going and prefer the real
			// answer that follows.
			name: "leading example JSON object is superseded by the real one that follows",
			raw:  `Here's an example of the format: {"text":"EXAMPLE, NOT THE ANSWER"} Now here's my answer: {"text":"hello"}`,
			want: `hello`,
		},
		{
			// Regression: a nested object under an unrelated key (e.g. "meta") is itself
			// syntactically valid JSON when decoded starting at its own brace, and would wrongly
			// decode into the target type too (unknown fields are ignored, not an error) —
			// decodeJSON must skip past the whole outer match's consumed span so this inner
			// brace is never probed as a separate candidate that could supersede it.
			name: "nested object under a different key is not mistaken for a separate candidate",
			raw:  `{"meta":{"text":"WRONG, NOT THE ANSWER"},"text":"hello"}`,
			want: `hello`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out struct {
				Text string `json:"text"`
			}
			if err := unmarshalRepaired(tt.raw, &out); err != nil {
				t.Fatalf("unmarshalRepaired: %v", err)
			}
			if out.Text != tt.want {
				t.Fatalf("got %q, want %q", out.Text, tt.want)
			}
		})
	}
}

// TestUnmarshalRepaired_MixedLeavesInSameResponse is the first ch01_slides.md regression: a
// single response with one bullet already correctly JSON-escaped ("\\(x\\)", observed from
// gpt-5.6-luna) and a separate, unrelated bullet containing a raw, ambiguous LaTeX command
// ("\frac", the common case from earlier models). Both bullets must come out correct — the
// already-valid one must not be mangled as collateral damage from repairing the other.
func TestUnmarshalRepaired_MixedLeavesInSameResponse(t *testing.T) {
	t.Parallel()

	raw := `{"bullets":["inline math \\(x\\) here","separately \frac{1}{2} elsewhere"]}`
	var out struct {
		Bullets []string `json:"bullets"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := []string{`inline math \(x\) here`, `separately \frac{1}{2} elsewhere`}
	if len(out.Bullets) != 2 || out.Bullets[0] != want[0] || out.Bullets[1] != want[1] {
		t.Fatalf("got %q, want %q", out.Bullets, want)
	}
}

// TestUnmarshalRepaired_MixedWithinSameString is the second, harder ch01_slides.md regression: a
// SINGLE bullet mixing an already-correctly-escaped "\\(" pair with a raw, unescaped "\tan" later
// in the very same string (observed for real: "For \\(\mathbf v=(v_1,v_2)\\), use
// \\(\theta=\tan^{-1}(v_2/v_1)\\)."). This is exactly the case TestUnmarshalRepaired_
// MixedLeavesInSameResponse's own history called out as unfixable at the leaf level — "you can't
// cherry-pick within one string" — which fixControlByteArtifacts now can, by repairing the
// specific control byte \tan decodes into rather than choosing whether to trust or repair the
// whole string.
func TestUnmarshalRepaired_MixedWithinSameString(t *testing.T) {
	t.Parallel()

	raw := `{"text":"For \\(\\mathbf v=(v_1,v_2)\\), use \\(\theta=\tan^{-1}(v_2/v_1)\\)."}`
	var out struct {
		Text string `json:"text"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := `For \(\mathbf v=(v_1,v_2)\), use \(\theta=\tan^{-1}(v_2/v_1)\).`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

// TestUnmarshalRepaired_NulByteArtifactEndToEnd is a regression for a bug caught in PR
// review: stripStrayControlBytes deletes any C0 control byte XML disallows, including NUL
// (0x00) -- the exact byte repairNulBytes exists to restore to a backslash (see its own doc
// comment). Before fixDecodedLeaf called repairNulBytes itself, repairNulBytes was only ever
// invoked later, in outline.go, on text that had already passed through fixDecodedLeaf during
// JSON decode -- by which point stripStrayControlBytes had already silently deleted the NUL,
// leaving repairNulBytes nothing to find. This exercises the real pipeline entry point
// (unmarshalRepaired), not repairNulBytes in isolation, so it would have caught that gap.
func TestUnmarshalRepaired_NulByteArtifactEndToEnd(t *testing.T) {
	t.Parallel()

	raw := `{"text":"solve \u0000mathbf{v}=0"}`
	var out struct {
		Text string `json:"text"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := `solve \mathbf{v}=0`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

// TestFixControlByteArtifacts_Table covers fixControlByteArtifacts directly: each of the five
// ambiguous control bytes restored to its backslash-letter pair, a clean string left untouched
// (changed=false), and — the actual bug this function exists to fix — a control byte and an
// already-correct backslash pair coexisting in the very same string, both ending up right.
func TestFixControlByteArtifacts_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		in          string
		wantFixed   string
		wantChanged bool
	}{
		{name: "clean string, nothing to fix", in: `\(x\)`, wantFixed: `\(x\)`, wantChanged: false},
		{name: "backspace byte restored", in: "\begin", wantFixed: `\begin`, wantChanged: true},
		{name: "form feed byte restored", in: "\frac", wantFixed: `\frac`, wantChanged: true},
		{name: "newline byte restored", in: "gradient \nabla f", wantFixed: `gradient \nabla f`, wantChanged: true},
		{name: "carriage return byte restored", in: "\rangle", wantFixed: `\rangle`, wantChanged: true},
		{name: "tab byte restored", in: "\tan", wantFixed: `\tan`, wantChanged: true},
		{
			// The ch01_slides.md regression: a raw, unescaped "\theta" (decodes to a tab byte)
			// sitting in the very same string as an already-correctly-escaped "\\(" pair — no
			// per-leaf repair-or-trust choice can get both right at once, only fixing the specific
			// control byte can. The Go source below uses a literal tab byte (typed as \t in the
			// source) exactly where json.Unmarshal would have produced one from a raw "\theta".
			name:        "control byte and an already-correct backslash pair in the same string",
			in:          "For \\(\\mathbf v\\), use \\(\tan^{-1}(x)\\).",
			wantFixed:   `For \(\mathbf v\), use \(\tan^{-1}(x)\).`,
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixed, changed := fixControlByteArtifacts(tt.in)
			if fixed != tt.wantFixed || changed != tt.wantChanged {
				t.Fatalf("got (%q, %v), want (%q, %v)", fixed, changed, tt.wantFixed, tt.wantChanged)
			}
		})
	}
}

// TestStripStrayControlBytes_Table covers stripStrayControlBytes directly: an XML-invalid C0
// control byte (observed in practice: ch07_slides.md had this exact byte where backslash-open-
// paren/backslash-close-paren math delimiters belonged, from a slide-generation LLM call
// emitting a valid-but-wrong 4-hex-digit JSON unicode escape) gets removed; tab/newline/
// carriage-return -- the three C0 bytes XML actually permits -- survive untouched; and a
// clean string reports changed=false.
func TestStripStrayControlBytes_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		in          string
		wantFixed   string
		wantChanged bool
	}{
		{name: "clean string, nothing to strip", in: `\(x\)`, wantFixed: `\(x\)`, wantChanged: false},
		{
			name:        "XML-invalid control byte removed (the ch07_slides.md regression)",
			in:          "the norm \x05mathbf{u}\x05 is nonnegative",
			wantFixed:   "the norm mathbf{u} is nonnegative",
			wantChanged: true,
		},
		{
			name:        "a different XML-invalid control byte is also removed",
			in:          "bell\x07ring",
			wantFixed:   "bellring",
			wantChanged: true,
		},
		{
			name:        "tab, newline, and carriage return survive untouched (XML permits them)",
			in:          "a\tb\nc\rd",
			wantFixed:   "a\tb\nc\rd",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixed, changed := stripStrayControlBytes(tt.in)
			if fixed != tt.wantFixed || changed != tt.wantChanged {
				t.Fatalf("got (%q, %v), want (%q, %v)", fixed, changed, tt.wantFixed, tt.wantChanged)
			}
		})
	}
}

// TestCollapseOverDoubledBackslashes_Table covers collapseOverDoubledBackslashes directly: an
// over-doubled pair not followed by whitespace collapses to one backslash, a genuine matrix row
// separator (followed by whitespace) is left alone, and a clean string with no double-backslash at
// all reports changed=false.
func TestCollapseOverDoubledBackslashes_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		in          string
		wantFixed   string
		wantChanged bool
	}{
		{name: "no double backslash at all", in: `\(x\)`, wantFixed: `\(x\)`, wantChanged: false},
		{
			name:        "over-doubled pair before a paren collapses to one",
			in:          `\\(c\\)`,
			wantFixed:   `\(c\)`,
			wantChanged: true,
		},
		{
			name:        "over-doubled pair before a letter collapses to one",
			in:          `\\mathbb R^n`,
			wantFixed:   `\mathbb R^n`,
			wantChanged: true,
		},
		{
			// A genuine matrix row separator: two literal backslashes immediately followed by a
			// space — the one legitimate reason this domain wants two literal backslashes back to
			// back (see repairMatrixRowSeparators) — must survive uncollapsed.
			name:        "doubled pair before whitespace (matrix row separator) is left alone",
			in:          `\begin{bmatrix}3\\ -2\end{bmatrix}`,
			wantFixed:   `\begin{bmatrix}3\\ -2\end{bmatrix}`,
			wantChanged: false,
		},
		{
			name:        "doubled pair at the very end of the string (no following byte) collapses",
			in:          `\\`,
			wantFixed:   `\`,
			wantChanged: true,
		},
		{
			// Caught in PR review: a matrix row separator that was itself already correctly
			// escaped in the raw response gets re-doubled right along with everything else
			// when repairJSONEscapes runs on the whole response, landing here as four
			// consecutive backslashes before whitespace instead of two. Collapsing only the
			// first pair (the old behavior) left three backslashes -- invalid LaTeX; the run
			// must halve down to the original two.
			name:        "re-doubled matrix row separator (four backslashes before whitespace) halves to two",
			in:          `\begin{bmatrix}3\\\\ -2\end{bmatrix}`,
			wantFixed:   `\begin{bmatrix}3\\ -2\end{bmatrix}`,
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixed, changed := collapseOverDoubledBackslashes(tt.in)
			if fixed != tt.wantFixed || changed != tt.wantChanged {
				t.Fatalf("got (%q, %v), want (%q, %v)", fixed, changed, tt.wantFixed, tt.wantChanged)
			}
		})
	}
}

// TestUnmarshalRepaired_OverDoubledByUnrelatedInvalidEscape is the ch01_slides.md slide-23/26
// regression: a single raw, unescaped LaTeX command using a letter JSON doesn't recognize as a
// valid escape at all ("\mathbb" — as opposed to "\tan", individually valid but ambiguous; see
// TestUnmarshalRepaired_MixedWithinSameString) is enough to make direct decode of the *entire*
// response fail outright, forcing the whole raw response through repairJSONEscapes — which then
// also re-doubles an unrelated, already-correctly-escaped "\\(" pair elsewhere in that same
// response as collateral damage. Both must come out right: "\mathbb" restored via the
// repairJSONEscapes fallback (which correctly un-mangles genuinely raw single backslashes), and
// "\(" collapsed back down from the fallback's over-doubling.
func TestUnmarshalRepaired_OverDoubledByUnrelatedInvalidEscape(t *testing.T) {
	t.Parallel()

	raw := `{"text":"A vector in \\(\mathbb R^3\\) requires three components."}`
	var out struct {
		Text string `json:"text"`
	}
	if err := unmarshalRepaired(raw, &out); err != nil {
		t.Fatalf("unmarshalRepaired: %v", err)
	}
	want := `A vector in \(\mathbb R^3\) requires three components.`
	if out.Text != want {
		t.Fatalf("got %q, want %q", out.Text, want)
	}
}

// TestRepairDecodedArtifacts_Map covers the map-valued branch of repairDecodedArtifacts directly
// via reflection, since no current response struct in this package actually has a map-typed field
// to exercise it through unmarshalRepaired end-to-end — it's written generically to walk any of
// this package's response shapes, not just the ones in use today, so the branch is real and worth
// covering on its own rather than only through the shapes that happen to exist right now.
func TestRepairDecodedArtifacts_Map(t *testing.T) {
	t.Parallel()

	m := map[string]string{"clean": `\(x\)`, "dirty": "gradient \nabla f"}
	repairDecodedArtifacts(reflect.ValueOf(m))

	if m["clean"] != `\(x\)` {
		t.Fatalf(`clean leaf changed: got %q, want %q`, m["clean"], `\(x\)`)
	}
	if m["dirty"] != `gradient \nabla f` {
		t.Fatalf(`dirty leaf not repaired: got %q, want %q`, m["dirty"], `gradient \nabla f`)
	}
}
