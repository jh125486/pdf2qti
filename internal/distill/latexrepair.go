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
// characters). The optional second backslash is captured so repairMatrixRowSeparators's callback
// can tell an already-correct separator (leave alone) from a bare one (needs doubling) without
// lookaround, which RE2 doesn't support.
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
