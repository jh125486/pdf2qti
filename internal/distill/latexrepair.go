package distill

import (
	"regexp"
	"strings"
)

// reMatrixEnvBegin and reMatrixEnvEnd bound the array-like LaTeX environments
// repairMatrixRowSeparators looks inside — the ones that use a bare "\\" to separate rows.
// Matched independently rather than as a single begin...end pattern with a backreference, since
// Go's regexp package (RE2) supports neither backreferences nor lookaround; a begin/end pair
// naming different environments is vanishingly unlikely in practice, and even if the model ever
// produced one, fixing row separators inside it is still the correct behavior either way.
var (
	reMatrixEnvBegin = regexp.MustCompile(`\\begin\{(?:[bBpPvV]?matrix|cases)\}`)
	reMatrixEnvEnd   = regexp.MustCompile(`\\end\{(?:[bBpPvV]?matrix|cases)\}`)
)

// reRowSeparator matches a run of one or two backslashes immediately followed by whitespace — a
// LaTeX row separator, correct only when doubled ("\\\\" in Go source, two literal backslash
// characters). The optional second backslash lets fixRowSeparators distinguish an already-correct
// separator (leave alone) from a bare one (needs doubling) without lookaround, which RE2 doesn't
// support; the callback uses the match length to decide.
var reRowSeparator = regexp.MustCompile(`\\\\?[ \t\n]`)

// repairMatrixRowSeparators fixes a LaTeX matrix row separator that's missing its second
// backslash — observed in practice against the real OpenAI API: the same model, across different
// completions, sometimes writes the correct doubled separator ("\begin{bmatrix} a \\ b
// \end{bmatrix}" as literal text, i.e. two backslash characters before "b") and sometimes a bare
// single one ("\begin{bmatrix} a \ b \end{bmatrix}", one backslash) — both are syntactically valid
// JSON string content, so this isn't a JSON-parsing bug (see jsonrepair.go for that class of
// issue) and unmarshalRepaired can't catch it; it's the model being inconsistent about LaTeX
// syntax itself.
//
// Deliberately scoped to text inside \begin{ENV}...\end{ENV} blocks for known array-like
// environments (bmatrix, pmatrix, vmatrix, Bmatrix, Vmatrix, matrix, cases) rather than fixing
// any bare "\ " anywhere — "\ " (backslash-space) is also a legitimate LaTeX command outside a
// matrix (an explicit space), and doubling it there would silently change its meaning. Fixing
// only inside a matrix environment, where a bare "\" followed by whitespace has no other
// legitimate meaning, keeps this a safe, unambiguous transformation rather than a general (and
// much riskier) "fix any missing LaTeX backslash" tool.
func repairMatrixRowSeparators(s string) string {
	begins := reMatrixEnvBegin.FindAllStringIndex(s, -1)
	if len(begins) == 0 {
		return s
	}

	var b strings.Builder
	pos := 0
	for _, beginLoc := range begins {
		if beginLoc[0] < pos {
			continue // inside an already-processed block
		}
		endLoc := reMatrixEnvEnd.FindStringIndex(s[beginLoc[1]:])
		if endLoc == nil {
			continue // unterminated environment; leave the rest of s untouched
		}
		blockEnd := beginLoc[1] + endLoc[1]

		b.WriteString(s[pos:beginLoc[0]])
		b.WriteString(fixRowSeparators(s[beginLoc[0]:blockEnd]))
		pos = blockEnd
	}
	b.WriteString(s[pos:])
	return b.String()
}

// fixRowSeparators doubles every bare (single-backslash) row separator within block, leaving
// already-doubled ones untouched.
func fixRowSeparators(block string) string {
	return reRowSeparator.ReplaceAllStringFunc(block, func(m string) string {
		if len(m) == 3 { // "\\" + whitespace: already doubled
			return m
		}
		return "\\" + m // "\" + whitespace: bare, needs doubling
	})
}

// nulByte is the literal NUL character (U+0000) repairNulBytes looks for, spelled out via
// rune conversion rather than an embedded raw control byte in this file's own source text.
var nulByte = string(rune(0))

// repairNulBytes replaces an embedded NUL byte with a literal backslash — observed in practice
// against the real OpenAI API (gpt-5.6-luna and gpt-5.6-terra specifically): a NUL byte appears
// exactly where a LaTeX command's leading backslash should be, with the command's letters intact
// right after (e.g. a NUL directly before "mathbf{v}", or a NUL on both sides of "(0,0)").
// jsonrepair.go's decodeJSON/repairJSONEscapes were traced and are not the source — both
// correctly double an arbitrary unrecognized backslash escape like "\(" or "\m" rather than
// losing it, so this is consistent with the model itself emitting a literal backslash-u-0000
// unicode escape as a spurious artifact (a syntactically valid JSON escape our pipeline has no
// reason to distrust on its own, unlike a LaTeX-command-shaped "\uXXXX" — see
// isValidUnicodeEscape's doc comment in jsonrepair.go on that legitimate case).
//
// Fixed here, after decoding, rather than by teaching jsonrepair.go to treat backslash-u-0000 as
// suspicious: that file's unicode-escape handling exists specifically to preserve genuine
// non-ASCII math symbols models sometimes emit that way (e.g. an escaped "not equal" sign), and
// narrowing it risks regressing that legitimate case. A NUL byte, unlike those, has no legitimate
// use in slide content under any circumstance, so replacing it unconditionally here — regardless
// of exactly how it got there — is safe.
func repairNulBytes(s string) string {
	if !strings.Contains(s, nulByte) {
		return s
	}
	return strings.ReplaceAll(s, nulByte, `\`)
}
