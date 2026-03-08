package extract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/extract"
)

func TestExtractText_MissingFile(t *testing.T) {
	_, err := extract.ExtractText("testdata/nonexistent.pdf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExtractText_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := extract.ExtractText(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "PDF content extracted from") {
		t.Errorf("expected stub message, got %q", result)
	}
}

func TestExtractText_WithParenContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.pdf")
	// Fake PDF data with parenthesized strings (PDF text operator format)
	content := []byte("BT (Hello World from PDF) Tj ET")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := extract.ExtractText(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Hello World from PDF") {
		t.Errorf("expected extracted text, got %q", result)
	}
}
