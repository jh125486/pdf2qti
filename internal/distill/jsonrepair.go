package distill

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

// controlByteToEscapeLetter maps each RFC 8259 control byte a raw, unescaped LaTeX command
// starting with the same letter ("\begin", "\frac", "\nabla"/"\neq"/"\notin", "\right"/"\rangle",
// "\times"/"\theta"/"\tan") decodes into, back to the letter that followed the backslash in the
// original (invalid, but json.Unmarshal-tolerated) escape — the exact inverse of RFC 8259's own
// \b \f \n \r \t table. json.Unmarshal decodes a raw, unescaped "\theta" as a literal tab byte
// (0x09) followed by "heta": syntactically valid JSON, semantically wrong, since this domain never
// legitimately wants a literal control byte in decoded content (see repairJSONEscapes's doc
// comment) — only the LaTeX command that byte-plus-remaining-letters was actually meant to be.
// fixControlByteArtifacts restores it by replacing the byte with '\' + this letter, recovering
// "\theta" exactly.
var controlByteToEscapeLetter = map[byte]byte{
	'\b': 'b',
	'\f': 'f',
	'\n': 'n',
	'\r': 'r',
	'\t': 't',
}

// repairDecodedArtifacts walks v — a value just populated by a successful decodeJSON(raw, v) —
// applying fixDecodedLeaf to every string it finds (structs, slices, and maps, via reflection).
//
// This runs unconditionally after every successful decode, direct or repaired, rather than trying
// to decide up front whether a decode can be trusted at all — a decision earlier versions of this
// package made per-response, then per-leaf, and got wrong both times. A raw, unescaped "\theta"
// and an already-correctly-escaped "\\(" can appear in the very same string value (observed in
// practice: "For \\(\mathbf v=...\\), use \\(\theta=...\\)" — a properly-escaped inline-math
// delimiter pair sitting immediately next to a raw \theta in the same bullet), which no decision
// about whether to trust an entire response, or even an entire leaf, can ever get right for both
// at once: repairing the whole leaf via repairJSONEscapes fixes \theta but doubles the
// already-correct \\( right next to it; trusting the leaf as-is keeps \\( correct but leaves
// \theta as a raw tab byte. Fixing the *specific artifact*, wherever it lands, is the only
// operation narrow enough to get both right — the same insight repairNulBytes (latexrepair.go)
// already applies to a different decode artifact.
func repairDecodedArtifacts(v reflect.Value) {
	switch v.Kind() { //nolint:exhaustive // only the kinds this package's response structs actually use need walking; every other Kind has nothing to repair
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			repairDecodedArtifacts(v.Elem())
		}
	case reflect.String:
		if v.CanSet() {
			if fixed, changed := fixDecodedLeaf(v.String()); changed {
				v.SetString(fixed)
			}
		}
	case reflect.Struct:
		for _, field := range v.Fields() {
			repairDecodedArtifacts(field)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			repairDecodedArtifacts(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			mv := v.MapIndex(k)
			if mv.Kind() != reflect.String {
				continue
			}
			if fixed, changed := fixDecodedLeaf(mv.String()); changed {
				v.SetMapIndex(k, reflect.ValueOf(fixed))
			}
		}
	}
}

// fixDecodedLeaf applies fixControlByteArtifacts, repairNulBytes, collapseOverDoubledBackslashes,
// and stripStrayControlBytes to s in turn, reporting whether any of them changed anything.
//
// repairNulBytes must run before stripStrayControlBytes: a NUL byte is itself an
// isXMLInvalidControlByte byte, so stripStrayControlBytes would otherwise silently delete the
// very artifact repairNulBytes exists to restore to a backslash — caught in PR review after
// stripStrayControlBytes was added without noticing it made repairNulBytes (previously only
// called later, on already-decoded bullet text in outline.go) unreachable in the real pipeline:
// by the time that later call ran, the NUL was already gone.
func fixDecodedLeaf(s string) (fixed string, changed bool) {
	s, c1 := fixControlByteArtifacts(s)
	beforeNulRepair := s
	s = repairNulBytes(s)
	c2 := s != beforeNulRepair
	s, c3 := collapseOverDoubledBackslashes(s)
	s, c4 := stripStrayControlBytes(s)
	return s, c1 || c2 || c3 || c4
}

// fixControlByteArtifacts replaces every controlByteToEscapeLetter byte in s with '\' plus its
// mapped letter, reporting whether it changed anything.
func fixControlByteArtifacts(s string) (fixed string, changed bool) {
	if !strings.ContainsAny(s, "\b\f\n\r\t") {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		if letter, ok := controlByteToEscapeLetter[s[i]]; ok {
			b.WriteByte('\\')
			b.WriteByte(letter)
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), true
}

// isXMLInvalidControlByte reports whether b is a C0 control byte XML 1.0 disallows in element
// content — every byte below 0x20 except tab/newline/carriage-return, the three XML explicitly
// permits (https://www.w3.org/TR/xml/#charsets).
func isXMLInvalidControlByte(b byte) bool {
	return b < 0x20 && b != '\t' && b != '\n' && b != '\r'
}

// stripStrayControlBytes removes every isXMLInvalidControlByte byte from s, reporting whether it
// changed anything. Observed in practice: a slide-generation LLM call emitting a genuine, valid
// JSON backslash-u-plus-4-hex-digit unicode escape (not a raw unescaped backslash — that's
// fixControlByteArtifacts's five-byte table above) for a low control-code point in place of
// intended content, e.g. inline-math delimiters replaced by a literal control byte. This domain
// never legitimately wants any control
// byte in decoded prose (see controlByteToEscapeLetter's doc comment) — and beyond being wrong
// content, a byte like this landing in pdf2qti's PPTX output isn't just cosmetic: OOXML is XML,
// and an XML-invalid byte in a run of text can make PowerPoint refuse to open the file rather
// than merely rendering oddly. Runs last in fixDecodedLeaf, after fixControlByteArtifacts has
// already turned the five bytes it can explain back into their letter-pair escapes, so nothing
// this function removes was ever a legitimate, explainable artifact.
func stripStrayControlBytes(s string) (fixed string, changed bool) {
	hasStray := false
	for i := 0; i < len(s); i++ {
		if isXMLInvalidControlByte(s[i]) {
			hasStray = true
			break
		}
	}
	if !hasStray {
		return s, false
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !isXMLInvalidControlByte(s[i]) {
			b = append(b, s[i])
		}
	}
	return string(b), true
}

// collapseOverDoubledBackslashes replaces a decoded "\\" (two literal backslash characters) not
// immediately followed by whitespace with a single backslash, reporting whether it changed
// anything.
//
// repairJSONEscapes doubles every raw backslash in the *entire* raw response whenever it runs at
// all — and it runs unconditionally on the whole response the moment any single raw, unescaped
// LaTeX command anywhere uses a letter JSON doesn't recognize as a valid escape at all
// ("\mathbf", "\ldots", "\mathbb" — as opposed to "\theta"/"\tan"/etc., which are individually
// valid JSON escapes and merely semantically ambiguous; see fixControlByteArtifacts for that
// case). A single stray "\mathbf" anywhere in a response is enough to force direct decode to fail
// outright (json.Unmarshal errors on the invalid escape), sending the *whole* raw response through
// repairJSONEscapes — which then also re-doubles any backslash pair elsewhere in that same
// response that was already correctly, validly escaped (e.g. "\\(" for a slide's inline math),
// since repairJSONEscapes has no way to tell "one raw backslash needing doubling" apart from "two
// raw backslashes that already formed a complete, correct escape." The result: an already-correct
// single backslash decodes to two.
//
// This domain has no legitimate reason to want two literal decoded backslashes back to back except
// a LaTeX matrix row separator (see repairMatrixRowSeparators), which is always immediately
// followed by whitespace — collapsing everything else back down to one backslash undoes
// repairJSONEscapes's over-doubling without touching that one legitimate case. Safe to apply to
// every successful decode, not just ones that went through repairJSONEscapes: a direct decode of
// already-valid JSON never produces two consecutive literal backslashes in its output at all
// (json.Unmarshal always collapses a valid "\\" escape to exactly one), so this is a no-op there.
//
// Over-doubling doesn't stop at pairs: a matrix separator that was itself already correctly
// escaped in the raw response ("\\\\ " in raw JSON, decoding to the legitimate "\\ ") gets
// re-doubled right along with everything else when repairJSONEscapes runs, landing here as four
// consecutive backslashes before whitespace instead of two. So this walks whole runs of
// consecutive backslashes rather than isolated pairs: a run of exactly two immediately before
// whitespace is left alone (the untouched, legitimate case above), and every other run — including
// a longer whitespace-preceded one — halves, since over-doubling doubles every backslash in the
// run uniformly.
//
// Known limitation, deliberately not fixed here (raised in a Fable-assisted review of this
// package): a compact-notation matrix row separator directly abutting a cell that's itself a
// single-backslash LaTeX command (e.g. "...&\lambda_1\\lambda_2&..." with no whitespace around
// the separator) can decode to an odd-length run this function can't correctly halve — the
// correct split depends on which raw encoding the model actually used for the separator (bare vs.
// already-doubled), information this function has no way to recover from the decoded run alone.
// The tempting-looking fix — leave a run alone whenever it's followed by a multi-letter command
// name instead of collapsing it — was checked against this function's own primary, most-common
// case (a single over-doubled command anywhere in prose, e.g. a decoded "\\mathbf" that must
// still collapse to "\mathbf") and would silently break it: that's the exact same shape (a
// 2-backslash run immediately followed by a multi-letter word) this function exists to fix, with
// no local signal to tell the two apart. Matrix-context awareness — which repairMatrixRowSeparators
// has and this function doesn't — is the only place that distinction could be made correctly; not
// attempted here given the risk of regressing the far more common case for a narrower one.
func collapseOverDoubledBackslashes(s string) (fixed string, changed bool) {
	if !strings.Contains(s, `\\`) {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s))
	didChange := false
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '\\' {
			j++
		}
		n := j - i // length of this run of consecutive backslashes
		next := byte(0)
		if j < len(s) {
			next = s[j]
		}
		// A run of exactly two immediately before whitespace is a genuine matrix row separator
		// (see the doc comment above) and must survive untouched; every other run — including a
		// longer whitespace-preceded run, which is that same separator re-doubled by
		// repairJSONEscapes — halves, since over-doubling doubles every backslash uniformly.
		keep := n
		if n != 2 || next != ' ' && next != '\t' && next != '\n' {
			keep = (n + 1) / 2
		}
		for range keep {
			b.WriteByte('\\')
		}
		if keep != n {
			didChange = true
		}
		i = j
	}
	if !didChange {
		return s, false
	}
	return b.String(), true
}

// unmarshalRepaired parses raw LLM JSON output into v: a direct parse is tried first and used if
// it succeeds, falling back to repairJSONEscapes only if it doesn't (see repairJSONEscapes's doc
// comment for why a direct parse routinely fails outright on raw, unescaped LaTeX containing a
// non-letter escape target like "\(" — invalid JSON, not merely ambiguous). Either way, a
// successful decode is always passed through repairDecodedArtifacts (which applies
// fixControlByteArtifacts and collapseOverDoubledBackslashes to every string leaf), safe to apply
// unconditionally since each only ever touches a byte pattern that could never legitimately appear
// in this content otherwise (see each one's own doc comment).
func unmarshalRepaired(raw string, v any) error {
	if err := decodeJSON(raw, v); err == nil {
		repairDecodedArtifacts(reflect.ValueOf(v))
		return nil
	}
	if err := decodeJSON(repairJSONEscapes(raw), v); err != nil {
		return err
	}
	repairDecodedArtifacts(reflect.ValueOf(v))
	return nil
}

// decodeJSON parses the JSON object in s into v, tolerating prose a model adds around it despite
// being asked for "only JSON" — observed in practice against the real OpenAI API. Trailing prose
// (after a complete, valid object) is handled by json.Decoder.Decode itself, which stops after
// one value and ignores what follows. Leading prose needs more care than just trimming to the
// first '{': this package's prompts embed chapter source text full of LaTeX
// ("\begin{bmatrix}...\end{bmatrix}", "\text{...}", ...), so a preamble sentence that quotes or
// references any of that source material can contain a "decoy" '{' well before the real JSON
// object starts.
//
// decodeJSON scans s left to right for '{' candidates and keeps the LAST top-level one that
// decodes successfully — not the first. A decoy brace from quoted LaTeX source essentially never
// forms syntactically valid JSON on its own, so it just fails to decode and the next candidate is
// tried either way; but a model asked to produce a specific JSON shape can also preface its real
// answer with a complete, validly-decodable example or illustration of that shape ("Here's the
// format: {...} Now here's my answer: {...}") before writing the real one — and
// json.Decoder.Decode has no way to tell a structurally-valid-but-wrong object like that apart
// from the real one (it can decode successfully into all zero/default values just as easily as a
// populated one). Preferring the last candidate, not the first, means such a leading example gets
// superseded by the real answer that follows it, rather than silently winning.
//
// After a candidate decodes successfully, the scan resumes only after the bytes that candidate's
// Decoder actually consumed (via InputOffset), not at the next raw '{' — otherwise every brace
// nested inside a successfully-decoded object (e.g. each entry of a real "outline": [{...}, {...}]
// answer) would itself be probed as a separate top-level candidate. Such a nested fragment decoded
// on its own is still syntactically valid JSON, just missing the outer field name(s) v's type
// expects; encoding/json silently ignores unknown fields rather than erroring, so it "succeeds"
// into a zero-value result and — under naive last-wins — would wrongly supersede the real answer
// that contains it. Skipping past each match's full span means only genuinely separate top-level
// objects are ever compared against each other.
//
// Each candidate decodes into a throwaway value of v's own type (via reflection) rather than v
// itself, so a candidate that fails partway through can never leave v holding a mix of an earlier
// successful decode's fields and a later failed attempt's partial ones; v is only ever assigned
// from a candidate that decoded cleanly in full.
func decodeJSON(s string, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("decodeJSON: v must be a non-nil pointer, got %T", v)
	}
	target := rv.Elem()

	err := errors.New("no '{' found in response")
	pos := 0
	for {
		rel := strings.IndexByte(s[pos:], '{')
		if rel < 0 {
			break
		}
		start := pos + rel

		candidate := reflect.New(target.Type())
		dec := json.NewDecoder(strings.NewReader(s[start:]))
		if decErr := dec.Decode(candidate.Interface()); decErr == nil {
			target.Set(candidate.Elem())
			err = nil
			// Resume past this whole matched object so its own nested braces are never probed
			// as separate candidates.
			pos = start + int(dec.InputOffset())
			continue
		} else if err != nil {
			// Only remember a failure's error while nothing has succeeded yet — once a real
			// candidate has decoded, a later candidate's failure isn't worth reporting over it.
			err = decErr
		}
		pos = start + 1
	}
	return err
}
