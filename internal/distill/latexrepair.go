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

// reLoneCellDigits and reLoneCellLetter identify a "lone cell" token — a single matrix cell's
// entire content — immediately following a bare row separator with no whitespace at all (see
// loneCellTokenLen). A leading "-" is included for a negative numeric or single-letter-variable
// cell (e.g. "-1", "-k").
var (
	reLoneCellDigits = regexp.MustCompile(`^-?\d+`)
	reLoneCellLetter = regexp.MustCompile(`^-?[A-Za-z]`)
)

// loneCellTokenLen returns the length of a safely-identifiable "lone cell" token at the start of
// s — a signed integer, or a single-letter variable name — or 0 if s doesn't begin with one.
//
// A signed integer is unambiguous regardless of what follows it: LaTeX macro names are never
// digits. A single letter is only treated as a lone cell if it is NOT immediately followed by
// another letter or digit — "theta" in "\theta" must never be mistaken for the single-letter cell
// "t" followed by unrelated content "heta", since \theta is a real, correct LaTeX command whose
// single leading backslash must stay exactly as-is.
func loneCellTokenLen(s string) int {
	if m := reLoneCellDigits.FindString(s); m != "" {
		return len(m)
	}
	if m := reLoneCellLetter.FindString(s); m != "" {
		if len(m) < len(s) && isAlnumByte(s[len(m)]) {
			return 0 // part of a longer identifier/command name (e.g. "theta") -- leave alone
		}
		return len(m)
	}
	return 0
}

func isAlnumByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// repairMatrixRowSeparators fixes a LaTeX matrix row separator that's missing its second
// backslash — observed in practice against the real OpenAI API: the same model, across different
// completions, sometimes writes the correct doubled separator ("\begin{bmatrix} a \\ b
// \end{bmatrix}" as literal text, i.e. two backslash characters before "b") and sometimes a bare
// single one ("\begin{bmatrix} a \ b \end{bmatrix}", one backslash) — both are syntactically valid
// JSON string content, so this isn't a JSON-parsing bug (see jsonrepair.go for that class of
// issue) and unmarshalRepaired can't catch it; it's the model being inconsistent about LaTeX
// syntax itself. A bare separator with no surrounding whitespace at all is just as common in
// compact matrix notation (e.g. "\begin{bmatrix}1&0\0&-1\end{bmatrix}", a bare "\" directly
// abutting the next row's first cell "0") — see loneCellTokenLen — and pandoc silently fails to
// convert the whole formula to OOXML math when this goes unfixed (ch05/ch09/ch10's "1&0\0&-1"-
// style 2x2 rotation/reflection/shear matrices, caught by comparing math-span counts in generated
// slide Markdown against actual <m:oMath> elements in the rendered PPTX — nothing in pdf2qti logs
// a per-formula conversion failure, see mathRunXML's doc comment on why that fallback is silent).
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

// fixRowSeparators doubles every bare (single-backslash) row separator within block — whether
// followed by whitespace or directly abutting the next row's first cell with no whitespace at all
// (see loneCellTokenLen) — leaving already-doubled ones, and anything else too ambiguous to
// safely touch (a run of more than two backslashes, or a bare backslash followed by neither
// whitespace nor a recognizable lone cell token), untouched.
//
// Scans block manually (rather than a single regexp, RE2's lack of lookaround aside) specifically
// to get backslash-run-length right: a naive per-backslash regexp risks matching the *second*
// backslash of an already-correct doubled separator that happens to be followed by a lone cell
// token too (e.g. the "0" in "...\\0&1..."), which would triple it into three backslashes —
// walking whole runs, exactly as jsonrepair.go's collapseOverDoubledBackslashes does for the same
// reason, avoids that.
func fixRowSeparators(block string) string {
	var b strings.Builder
	b.Grow(len(block))
	for i := 0; i < len(block); {
		if block[i] != '\\' {
			b.WriteByte(block[i])
			i++
			continue
		}
		j := i
		for j < len(block) && block[j] == '\\' {
			j++
		}
		n := j - i
		if n != 1 {
			b.WriteString(block[i:j]) // already doubled, or too ambiguous to guess at
			i = j
			continue
		}
		next := byte(0)
		if j < len(block) {
			next = block[j]
		}
		switch {
		case next == ' ' || next == '\t' || next == '\n':
			b.WriteString(`\\`)
			b.WriteByte(next)
			i = j + 1
		case loneCellTokenLen(block[j:]) > 0:
			tokenLen := loneCellTokenLen(block[j:])
			b.WriteString(`\\`)
			b.WriteString(block[j : j+tokenLen])
			i = j + tokenLen
		default:
			b.WriteByte('\\') // ambiguous (e.g. a real multi-letter LaTeX command): leave alone
			i = j
		}
	}
	return b.String()
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
