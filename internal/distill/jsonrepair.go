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

// ambiguousControlBytes are the actual RFC 8259 control bytes (backspace, form feed, newline,
// carriage return, tab) that a raw, unescaped LaTeX command starting with the same letter
// ("\begin", "\frac", "\nabla"/"\neq"/"\notin", "\right"/"\rangle", "\times"/"\theta"/"\tan")
// decodes into if json.Unmarshal is trusted at face value: syntactically valid JSON, semantically
// wrong, since this domain never legitimately wants a literal control byte in decoded content
// (see repairJSONEscapes's doc comment) — only the LaTeX command that byte-plus-remaining-letters
// was actually meant to be.
const ambiguousControlBytes = "\b\f\n\r\t"

// hasAmbiguousControlByte reports whether v — a value just populated by a successful
// decodeJSON(raw, v) — contains any ambiguousControlBytes anywhere in its string data (walking
// structs, slices, and maps via reflection). See unmarshalRepaired for why this checks the
// *decoded result* rather than scanning raw for a "\b"/"\f"/"\n"/"\r"/"\t" substring.
func hasAmbiguousControlByte(v reflect.Value) bool {
	switch v.Kind() { //nolint:exhaustive // only the kinds this package's response structs actually use need walking; every other Kind falls through to `return false` below, which is correct for them (nothing to scan)
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && hasAmbiguousControlByte(v.Elem())
	case reflect.String:
		return strings.ContainsAny(v.String(), ambiguousControlBytes)
	case reflect.Struct:
		for i := range v.NumField() {
			if hasAmbiguousControlByte(v.Field(i)) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if hasAmbiguousControlByte(v.Index(i)) {
				return true
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if hasAmbiguousControlByte(v.MapIndex(k)) {
				return true
			}
		}
	}
	return false
}

// unmarshalRepaired parses raw LLM JSON output into v, applying repairJSONEscapes only when
// needed: a direct parse is tried first and used as-is if it succeeds AND the result contains no
// ambiguousControlBytes; repair is used otherwise.
//
// repairJSONEscapes's doubling heuristic assumes the model never emits a correctly-escaped
// backslash on its own — true for the common case (raw, unescaped LaTeX) but not always: a model
// can occasionally emit a backslash that's already valid JSON (e.g. "\\overrightarrow", a genuine
// "\\" escape immediately followed by an unrelated LaTeX command). Running the repair
// unconditionally, as every caller of repairJSONEscapes did before this function existed, mangles
// that valid input: unaware "\\" was already a complete, correct escape, it doubles both
// backslashes independently and the result decodes to two literal backslashes instead of one
// ("\\overrightarrow" instead of "\overrightarrow").
//
// But trusting *any* successful direct parse is unsafe on its own: raw "\nabla" (etc.) parses
// successfully too, decoding into a newline byte plus "abla" — silently wrong, and exactly the
// bug class repairJSONEscapes exists to catch (see TestRepairJSONEscapes_LatexNablaNotNewline and
// siblings). Checking the decoded result for ambiguousControlBytes catches that case exactly as
// reliably as scanning raw text for a "\b"/"\f"/"\n"/"\r"/"\t" substring would — but scoped to
// what the parse actually produced, not merely what raw happens to contain anywhere in it.
//
// But re-decoding the *entire* raw response through repairJSONEscapes whenever any field looks
// suspicious has its own collateral-damage problem, observed in practice: gpt-5.6-luna properly
// JSON-escapes its own backslashes (e.g. "\\(" for a slide's inline math) far more consistently
// than earlier models did, but a single multi-bullet response routinely mixes an already-correct
// field like that with an unrelated, genuinely-raw command in a different bullet of the very same
// response (e.g. "\frac" — routine in math-heavy content). Discarding the whole direct decode over
// one bad field mangled every already-correct field as collateral damage too — 100% of a real
// generated deck's inline math delimiters ended up doubled this way. So the fallback below is
// applied per leaf, not per response: only the specific string fields hasAmbiguousControlByte
// actually flags are replaced with their repaired counterpart; every other field keeps whatever
// the direct, unrepaired decode produced.
func unmarshalRepaired(raw string, v any) error {
	if err := decodeJSON(raw, v); err != nil {
		return decodeJSON(repairJSONEscapes(raw), v)
	}

	direct := reflect.ValueOf(v)
	if !hasAmbiguousControlByte(direct) {
		return nil
	}

	repaired := reflect.New(direct.Elem().Type())
	if err := decodeJSON(repairJSONEscapes(raw), repaired.Interface()); err != nil {
		// The direct decode above already succeeded; keep it rather than discarding a working
		// result just because the repair fallback itself couldn't parse.
		return nil
	}
	mergeAmbiguousLeaves(direct, repaired)
	return nil
}

// mergeAmbiguousLeaves overwrites direct's string leaves with repaired's corresponding leaves
// wherever direct's own leaf contains an ambiguousControlByte — i.e. wherever that specific leaf's
// direct decode was untrustworthy — leaving every other leaf (the overwhelming majority, in
// practice) as whatever the direct, unrepaired decode already produced. direct and repaired must
// be reflect.Values of identical type, both already populated by a successful decodeJSON call.
func mergeAmbiguousLeaves(direct, repaired reflect.Value) {
	switch direct.Kind() { //nolint:exhaustive // only the kinds this package's response structs actually use need walking; every other Kind has nothing to merge
	case reflect.Pointer, reflect.Interface:
		if !direct.IsNil() {
			mergeAmbiguousLeaves(direct.Elem(), repaired.Elem())
		}
	case reflect.String:
		if direct.CanSet() && strings.ContainsAny(direct.String(), ambiguousControlBytes) {
			direct.SetString(repaired.String())
		}
	case reflect.Struct:
		for i := range direct.NumField() {
			mergeAmbiguousLeaves(direct.Field(i), repaired.Field(i))
		}
	case reflect.Slice, reflect.Array:
		for i := range direct.Len() {
			mergeAmbiguousLeaves(direct.Index(i), repaired.Index(i))
		}
	case reflect.Map:
		for _, k := range direct.MapKeys() {
			mv := direct.MapIndex(k)
			if mv.Kind() == reflect.String && strings.ContainsAny(mv.String(), ambiguousControlBytes) {
				direct.SetMapIndex(k, repaired.MapIndex(k))
			}
		}
	}
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
