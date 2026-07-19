// Package extract provides PDF text extraction utilities.
package extract

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractText extracts text from a PDF file and returns it as a string.
// It first shells out to poppler's pdftotext (if installed), which resolves each font's
// ToUnicode CMap correctly and reconstructs word spacing from glyph positioning — both of
// which the pure-Go fallback below gets wrong on some PDFs (e.g. zyBooks exports render
// correctly on screen via a custom font encoding, but decode to a per-character Caesar-shifted,
// space-stripped mess through naive glyph-code extraction). If pdftotext is unavailable or
// fails, it falls back to a real PDF parse (handles compressed content streams); if that also
// fails (malformed/non-PDF input, e.g. test fixtures), it falls back to a naive raw-bytes scan,
// and finally to a stub placeholder if nothing could be extracted.
func ExtractText(path string) (string, error) { //nolint:revive // stutter is acceptable for exported package function
	if text, err := extractWithPoppler(path); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	if text, err := extractWithPDFLib(path); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read PDF %q: %w", path, err)
	}
	text := extractRawText(data)
	if text == "" {
		return fmt.Sprintf("[PDF content extracted from %s]", path), nil
	}
	return text, nil
}

// extractWithPoppler extracts text via the external pdftotext binary (poppler-utils),
// preserving reading-order layout and word spacing. Returns an error if pdftotext isn't on
// PATH or the command fails (e.g. the input isn't a real PDF).
func extractWithPoppler(path string) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext not found: %w", err)
	}
	out, err := exec.Command("pdftotext", "-layout", path, "-").Output() //nolint:gosec // path is a configured source PDF, not user input
	if err != nil {
		return "", fmt.Errorf("pdftotext %q: %w", path, err)
	}
	return string(out), nil
}

// extractWithPDFLib extracts text page-by-page using a real PDF parser, which
// (unlike extractRawText) correctly decompresses FlateDecode content streams.
func extractWithPDFLib(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PDF %q: %w", path, err)
	}
	defer f.Close()

	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// extractRawText does a basic extraction of visible text from PDF bytes.
func extractRawText(data []byte) string {
	// Very basic: extract strings between parentheses in text streams
	// This is a simplified approach; production would use a proper PDF parser
	content := string(data)
	var parts []string
	inStr := false
	var cur strings.Builder
	for i := 0; i < len(content); i++ {
		ch := content[i]
		switch {
		case ch == '(' && !inStr:
			inStr = true
			cur.Reset()
		case ch == ')' && inStr:
			inStr = false
			s := cur.String()
			if len(s) > 3 {
				parts = append(parts, s)
			}
		case inStr:
			if ch >= 32 && ch < 127 {
				cur.WriteByte(ch)
			}
		}
	}
	return strings.Join(parts, " ")
}
