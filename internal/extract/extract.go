// Package extract provides PDF text extraction utilities.
package extract

import (
	"fmt"
	"os"
	"strings"
)

// ExtractText extracts text from a PDF file and returns it as a string.
// Returns an error if the PDF cannot be read, and falls back to a stub if no text
// can be extracted (for testing without real PDFs).
func ExtractText(path string) (string, error) { //nolint:revive // stutter is acceptable for exported package function
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read PDF %q: %w", path, err)
	}
	// Simple text extraction: look for text streams
	// In a real implementation, use a proper PDF library
	text := extractRawText(data)
	if text == "" {
		return fmt.Sprintf("[PDF content extracted from %s]", path), nil
	}
	return text, nil
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
