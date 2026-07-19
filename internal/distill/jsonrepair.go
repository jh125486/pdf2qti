package distill

import "strings"

// validJSONEscapes are the characters JSON allows immediately after a backslash inside a
// string literal (RFC 8259 §7).
const validJSONEscapes = `"\/bfnrtu`

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
