package extract_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/extract"
)

func TestExtractText_Table(t *testing.T) {
	t.Parallel()

	//nolint:gosec // Fixture strings and temp filenames are test data, not credentials.
	tests := []struct {
		name      string
		prepare   func(t *testing.T, dir string) string
		wantErr   bool
		wantToken string
	}{
		{
			name: "missing file",
			prepare: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "nonexistent.pdf")
			},
			wantErr: true,
		},
		{
			name: "empty file",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "empty.pdf")
				if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantToken: "PDF content extracted from",
		},
		{
			name: "extract paren text",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "fake.pdf")
				if err := os.WriteFile(path, []byte("BT (Hello World from PDF) Tj ET"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantToken: "Hello World from PDF",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := tt.prepare(t, dir)
			result, err := extract.ExtractText(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantToken != "" && !strings.Contains(result, tt.wantToken) {
				t.Fatalf("expected output to contain %q, got %q", tt.wantToken, result)
			}
		})
	}
}

// TestExtractText_PopplerPath drives extractWithPoppler (unexported) indirectly through
// ExtractText by controlling PATH, so both branches — pdftotext missing vs. present — are
// exercised deterministically regardless of whether the machine running the test actually has
// poppler-utils installed. Not t.Parallel(): each case mutates the process-wide PATH via
// t.Setenv, which forbids parallel ancestry.
func TestExtractText_PopplerPath(t *testing.T) {
	pdfPath := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(pdfPath, []byte("BT (irrelevant, pdftotext is stubbed) Tj ET"), 0o600); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // Fixture strings and stub script contents are test data, not credentials.
	tests := []struct {
		name        string
		stubScript  string // empty means no pdftotext stub on PATH at all
		wantToken   string
		wantExactly bool
	}{
		{
			name:       "pdftotext not on PATH falls through",
			stubScript: "",
			wantToken:  "irrelevant, pdftotext is stubbed",
		},
		{
			name:        "pdftotext succeeds and is used directly",
			stubScript:  "#!/bin/sh\nprintf 'poppler extracted text'\n",
			wantToken:   "poppler extracted text",
			wantExactly: true,
		},
		{
			name:       "pdftotext present but fails falls through",
			stubScript: "#!/bin/sh\nexit 1\n",
			wantToken:  "irrelevant, pdftotext is stubbed",
		},
		{
			name:       "pdftotext present but emits blank output falls through",
			stubScript: "#!/bin/sh\nprintf ''\n",
			wantToken:  "irrelevant, pdftotext is stubbed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not t.Parallel(): mutates PATH via t.Setenv (see doc comment above).
			binDir := t.TempDir()
			if tt.stubScript != "" {
				stubPath := filepath.Join(binDir, "pdftotext")
				if err := os.WriteFile(stubPath, []byte(tt.stubScript), 0o700); err != nil { //nolint:gosec // test-local executable stub, not sensitive
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", binDir)

			result, err := extract.ExtractText(pdfPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantExactly {
				if result != tt.wantToken {
					t.Fatalf("got %q, want exactly %q", result, tt.wantToken)
				}
				return
			}
			if !strings.Contains(result, tt.wantToken) {
				t.Fatalf("expected output to contain %q, got %q", tt.wantToken, result)
			}
		})
	}
}

// TestExtractText_PDFLibFallback exercises extractWithPDFLib's real page-parsing path: a
// syntactically valid classic-xref PDF that pdftotext (stubbed absent) can't touch, so
// ExtractText falls through to the pure-Go parser and must walk actual page/content objects.
func TestExtractText_PDFLibFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH-stubbing via a shell script assumes a POSIX shell")
	}

	//nolint:gosec // Fixture text is test data, not credentials.
	tests := []struct {
		name      string
		pdf       []byte
		wantToken string
	}{
		{
			name:      "single page with text content",
			pdf:       buildTestPDF(t, []string{"Hello from pdflib"}),
			wantToken: "Hello from pdflib",
		},
		{
			name:      "multiple pages concatenated",
			pdf:       buildTestPDF(t, []string{"Page one text", "Page two text"}),
			wantToken: "Page one text",
		},
		{
			// A Kids entry pointing at an object number that doesn't exist resolves to a null
			// Value, exercising extractWithPDFLib's page.V.IsNull() skip — the real page
			// alongside it must still come through.
			name:      "dangling page reference is skipped, real page still extracted",
			pdf:       buildTestPDFWithDanglingPage(t, "Only real page text"),
			wantToken: "Only real page text",
		},
		{
			// Contents pointing at a non-stream object (here, the Font dict) makes
			// GetPlainText fail on an otherwise-openable page, exercising extractWithPDFLib's
			// per-page error-continue branch. With no text recovered, ExtractText must fall
			// all the way through to the final stub placeholder rather than erroring out.
			name:      "page with unreadable content stream falls through to stub",
			pdf:       buildTestPDFWithBadContents(t),
			wantToken: "[PDF content extracted from",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not t.Parallel(): mutates PATH via t.Setenv (see TestExtractText_PopplerPath).
			t.Setenv("PATH", t.TempDir()) // no pdftotext on PATH: forces the pdflib fallback

			dir := t.TempDir()
			path := filepath.Join(dir, "real.pdf")
			if err := os.WriteFile(path, tt.pdf, 0o600); err != nil {
				t.Fatal(err)
			}

			result, err := extract.ExtractText(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result, tt.wantToken) {
				t.Fatalf("expected output to contain %q, got %q", tt.wantToken, result)
			}
		})
	}
}

// buildTestPDF assembles a minimal, syntactically valid classic-xref-table PDF (one object per
// page, each rendering a single Tj text-showing operator) so extractWithPDFLib's real
// pdf.Open/GetPlainText path can be exercised without vendoring a binary fixture. Byte offsets
// for the xref table are computed from the buffer as objects are written, rather than
// hardcoded, so the fixture can't silently drift out of sync with its own content.
func buildTestPDF(t *testing.T, pageTexts []string) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	// Object numbers: 1=Catalog, 2=Pages, 3=Font, then one Page + one Contents stream per
	// entry in pageTexts, interleaved as (4,5), (6,7), ...
	numObjs := 3 + 2*len(pageTexts)
	offsets := make([]int, numObjs+1) // 1-indexed; offsets[0] unused (free-list head)

	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	kids := make([]string, len(pageTexts))
	for i := range pageTexts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pageTexts)))
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	for i, text := range pageTexts {
		pageNum := 4 + 2*i
		contentsNum := pageNum + 1
		writeObj(pageNum, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> "+
				"/MediaBox [0 0 200 200] /Contents %d 0 R >>",
			contentsNum))

		content := fmt.Sprintf("BT /F1 24 Tf 10 100 Td (%s) Tj ET", text)
		writeObj(contentsNum, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", numObjs+1)
	for n := 1; n <= numObjs; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", numObjs+1, xrefOffset)

	return buf.Bytes()
}

// buildTestPDFWithDanglingPage is a one-off variant of buildTestPDF whose Pages/Kids array
// references an object number that's never defined, resolving to a null page Value.
func buildTestPDFWithDanglingPage(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	const numObjs = 5 // 1=Catalog, 2=Pages, 3=Font, 4=Page, 5=Contents (object 99 stays undefined)
	offsets := make([]int, numObjs+1)

	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [4 0 R 99 0 R] /Count 2 >>")
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(4, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> "+
		"/MediaBox [0 0 200 200] /Contents 5 0 R >>")
	content := fmt.Sprintf("BT /F1 24 Tf 10 100 Td (%s) Tj ET", text)
	writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", numObjs+1)
	for n := 1; n <= numObjs; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", numObjs+1, xrefOffset)

	return buf.Bytes()
}

// buildTestPDFWithBadContents is a one-off variant of buildTestPDF whose Page's /Contents
// points at a non-stream object (the Font dict), which pdf.Open resolves fine but
// GetPlainText fails to read ("stream not present").
func buildTestPDFWithBadContents(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	const numObjs = 4 // 1=Catalog, 2=Pages, 3=Font (doubles as the bogus Contents), 4=Page
	offsets := make([]int, numObjs+1)

	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [4 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(4, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> "+
		"/MediaBox [0 0 200 200] /Contents 3 0 R >>")

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", numObjs+1)
	for n := 1; n <= numObjs; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", numObjs+1, xrefOffset)

	return buf.Bytes()
}
