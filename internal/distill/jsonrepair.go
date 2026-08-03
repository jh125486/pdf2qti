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
//
// Backslash itself is deliberately excluded from this set, even though "\\" is a valid JSON
// escape for a single literal backslash. The model never emits properly JSON-escaped
// backslashes — it emits raw LaTeX — so a raw "\\" in its output is virtually always a LaTeX
// row-break ("\\" in \begin{bmatrix}...\\...\end{bmatrix}"), meaning two literal backslashes
// are wanted in the decoded string, not one. Treating "\\" as pre-escaped silently drops a
// backslash (each raw "\\" collapses to a single decoded backslash), which corrupted matrix
// row breaks into run-on rows. Excluding it means every backslash is independently doubled,
// so N raw backslashes round-trip through JSON as N literal backslashes.
const validJSONEscapes = `"/nu`

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
			// A valid 2-char escape (e.g. "\n" or "\""): consume both chars now so the second
			// one is never re-examined on its own as the start of a new (possibly invalid)
			// escape sequence.
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteString(`\\`)
	}
	return b.String()
}
