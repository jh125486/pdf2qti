package distill

import "strings"

// validJSONEscapes are the characters treated as legitimate immediately after a backslash.
// RFC 8259 §7 technically also allows "b" (backspace), "f" (form feed), "r" (carriage return),
// and "t" (tab) here, but this package's LLM prompts are full of LaTeX ("\begin{bmatrix}",
// "\frac{...}", "\times", "\theta", "\rangle", "\right"), and a model that forgets to
// double-escape a backslash before any of b/f/r/t is essentially always mid-LaTeX-command —
// never intending an actual control character (none of our content — slide bullets, distilled
// prose — has a legitimate reason to contain a literal backspace/form-feed/carriage-return/tab
// byte anyway). Treating them as invalid (and therefore doubling them) avoids silently
// corrupting output with control bytes; see TestRepairJSONEscapes_LatexBeginNotBackspace and
// TestRepairJSONEscapes_LatexTimesNotTab for the concrete failures this fixes.
const validJSONEscapes = `"\/nu`

// repairJSONEscapes doubles any backslash in s that isn't followed by a valid JSON escape
// character. LLMs asked for "a JSON object" routinely emit LaTeX inside string values (e.g.
// "\(x^2\)" or "\text{...}") without escaping the backslash, which otherwise breaks
// json.Unmarshal with "invalid character ... in string escape code".
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
		if i+1 < len(s) && strings.IndexByte(validJSONEscapes, s[i+1]) >= 0 {
			// A valid 2-char escape (e.g. "\\" or "\n"): consume both chars now so the second
			// one — which may itself be a backslash — is never re-examined on its own as the
			// start of a new (possibly invalid) escape sequence.
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteString(`\\`)
	}
	return b.String()
}
