package distill

import (
	"encoding/json"
	"strings"
)

// leaveAloneEscapes are the characters immediately after a backslash that are left as a
// genuine, load-bearing JSON escape rather than doubled. This package's LLM prompts are full
// of LaTeX ("\begin{bmatrix}", "\frac{...}", "\times", "\theta", "\nabla", "\neq", "\notin",
// "\underline", ...) and the model never actually double-escapes on purpose — it just emits
// raw LaTeX. So backslash-followed-by-ASCII-letter is always a LaTeX command, never a
// deliberate JSON control escape (this content — slide bullets, distilled prose — has no
// legitimate reason to contain a literal backspace/form-feed/newline/carriage-return/tab byte
// the model would intentionally escape). That includes "n": \nabla, \neq, \notin,
// \newcommand, \nonumber all start with backslash-n and must be doubled, not decoded as a
// newline; see TestRepairJSONEscapes_LatexNablaNotNewline.
//
// Only two non-letter escapes remain genuinely ambiguous-but-safe to leave alone:
//   - `"`: mandatory for JSON validity. Any literal double-quote in prose MUST appear as \" —
//     doubling it would decode to a literal backslash followed by a bare quote, terminating
//     the JSON string early and breaking virtually any prose containing a quotation mark.
//   - `/`: a legitimate JSON escape decoding to a literal `/`. Leaving it alone can never
//     produce invalid JSON, and in this domain a bare `/` (division, units like m/s) is far
//     more likely than a genuinely-intended literal "\/" in output.
//
// `\uXXXX` unicode escapes are handled separately (see isValidUnicodeEscape) rather than via
// this whitelist, because "u" alone is not enough to tell a genuine unicode escape (some
// models emit non-ASCII math symbols this way, e.g. "≠" for "≠") apart from the many
// LaTeX commands starting with u (\underline, \uparrow, \Uparrow, \uplus, \uline, \ulcorner,
// \uwave, \uproot, ...): it additionally requires validating the 4 trailing hex digits.
const leaveAloneEscapes = `"/`

// isHexDigit reports whether b is an ASCII hex digit (0-9, a-f, A-F).
func isHexDigit(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F')
}

// isValidUnicodeEscape reports whether s[i:] begins with a backslash-u escape followed by
// exactly 4 hex digits (a genuine JSON "\uXXXX" unicode escape), as opposed to a LaTeX command
// that merely starts with "u" (\underline, \uparrow, ...). s[i] must be '\\' and s[i+1] must
// be 'u'.
func isValidUnicodeEscape(s string, i int) bool {
	return i+5 < len(s) &&
		isHexDigit(s[i+2]) && isHexDigit(s[i+3]) && isHexDigit(s[i+4]) && isHexDigit(s[i+5])
}

// repairJSONEscapes doubles any backslash in s that isn't part of a genuine JSON escape
// (\", \/, or a validated \uXXXX unicode escape). LLMs asked for "a JSON object" routinely
// emit LaTeX inside string values (e.g. "\(x^2\)", "\text{...}", "\nabla f") without escaping
// the backslash, which otherwise breaks json.Unmarshal with "invalid character ... in string
// escape code" or, worse, silently decodes into control bytes plus mangled text.
func repairJSONEscapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			// Trailing/truncated backslash: nothing follows to form a valid escape.
			b.WriteString(`\\`)
			continue
		}
		next := s[i+1]
		if strings.IndexByte(leaveAloneEscapes, next) >= 0 {
			// A genuine \" or \/ escape: consume both chars now so the second one is never
			// re-examined on its own as the start of a new (possibly invalid) escape sequence.
			b.WriteByte(c)
			b.WriteByte(next)
			i++
			continue
		}
		if next == 'u' && isValidUnicodeEscape(s, i) {
			// A validated \uXXXX unicode escape: consume all 6 chars as-is.
			b.WriteString(s[i : i+6])
			i += 5
			continue
		}
		b.WriteString(`\\`)
	}
	return b.String()
}

// unmarshalRepaired parses raw LLM JSON output into v, applying repairJSONEscapes only if a
// direct, unmodified parse fails.
//
// repairJSONEscapes's doubling heuristic assumes the model never emits a correctly-escaped
// backslash on its own — true for the common case (raw, unescaped LaTeX) but not always: a model
// can occasionally emit a backslash that's already valid JSON (e.g. "\\overrightarrow", a
// genuine "\\" escape immediately followed by an unrelated LaTeX command). Running the repair
// unconditionally, as every caller of repairJSONEscapes did before this, mangles that valid
// input: unaware "\\" was already a complete, correct escape, it doubles both backslashes
// independently and the result decodes to two literal backslashes instead of one
// ("\\overrightarrow" instead of "\overrightarrow").
//
// Trying the unmodified raw text first means repair only ever touches responses that are
// actually broken, leaving already-valid JSON (however it happened to escape its backslashes)
// untouched.
func unmarshalRepaired(raw string, v any) error {
	if err := json.Unmarshal([]byte(raw), v); err == nil {
		return nil
	}
	return json.Unmarshal([]byte(repairJSONEscapes(raw)), v)
}
