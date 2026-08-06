package pptx_test

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/pptx"
)

// hasPandoc reports whether pandoc is on PATH, so math tests can assert real OMML output when
// it's available and graceful-fallback behavior otherwise, without failing in either environment.
func hasPandoc(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("pandoc")
	return err == nil
}

// stubPandoc puts an executable named "pandoc" (running script) on a fresh PATH-only directory
// and points PATH at it, so toOMML's exec.LookPath("pandoc") finds a deterministic stand-in
// instead of (or in addition to) whatever the real pandoc on this machine would do. An empty
// script leaves the directory empty, simulating pandoc being unavailable at all. Not usable from
// a t.Parallel() test: mutates PATH via t.Setenv.
func stubPandoc(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "pandoc"), []byte(script), 0o700); err != nil { //nolint:gosec // test-local executable stub
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// zipBytes builds an in-memory zip archive from name->content pairs, for feeding to a stub
// pandoc's stdout as fake docx output.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// catScript writes data to a fixture file under dir and returns a shell script that cats it to
// stdout, standing in for a stub pandoc's binary docx output. Uses an absolute path to cat
// rather than relying on PATH lookup: stubPandoc's caller overrides PATH down to just the stub
// directory (so exec.LookPath("pandoc") finds only the stub), which also makes `cat` on its own
// unresolvable from inside the script — it fails with "command not found" (exit 127), not the
// intended checksum/parse error, if left unqualified.
func catScript(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "out.docx")
	if err := os.WriteFile(fixture, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return "#!/bin/sh\n/bin/cat '" + fixture + "'\n"
}

// renderMathContent renders sampleContext() with slide[0]'s content overridden to content, and
// returns the resulting first content slide's XML plus any formula-conversion warnings Render
// reported.
func renderMathContent(t *testing.T, content string) (contentSlide string, warnings []string) {
	t.Helper()
	dc := sampleContext()
	dc.Slides[0].Content = content

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, baseTemplateEntries()); err != nil {
		t.Fatal(err)
	}
	warnings, err := pptx.Render(templatePath, dc, "", nil, outputPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(outEntries["ppt/slides/slide2.xml"]), warnings
}

// TestRender_MathConversion covers pptx.Render's LaTeX-to-OMML math conversion (math.go), both
// the real-pandoc path and every way toOMML/extractDocumentXML degrade gracefully instead of
// erroring. Every formula used across these cases must be unique: toOMML caches successful
// conversions in a package-level map keyed by formula text, shared across all tests in this
// binary regardless of t.Parallel() — reusing a formula between a "succeeds with real pandoc"
// case and a "must fall back" case lets the first poison the second's result via the cache,
// independent of that case's own PATH stubbing.
func TestRender_MathConversion(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		pathOverride bool   // stubs PATH via t.Setenv; the case can't run t.Parallel() when set
		pandocScript string // pandoc stub script content; empty + pathOverride means no pandoc on PATH at all
		verify       func(t *testing.T, contentSlide string, warnings []string)
	}{
		{
			name:    "inline math, real pandoc or graceful fallback",
			content: "Point 1\nSee \\(x^2 + y^2 = z^2\\) for details",
			verify: func(t *testing.T, contentSlide string, warnings []string) {
				t.Helper()
				if hasPandoc(t) {
					mustContainAll(t, "content slide with pandoc available", contentSlide, "mc:AlternateContent", "m:oMath", "m:sSup")
					if !strings.Contains(contentSlide, "mc:Fallback") {
						t.Fatalf("expected mc:Fallback alongside real math, got: %s", contentSlide)
					}
					if len(warnings) != 0 {
						t.Fatalf("expected no warnings on successful conversion, got: %v", warnings)
					}
				} else {
					mustContainAll(t, "content slide without pandoc", contentSlide, "x^2 + y^2 = z^2")
					if strings.Contains(contentSlide, "mc:AlternateContent") {
						t.Fatalf("expected no math markup without pandoc, got: %s", contentSlide)
					}
					mustContainAll(t, "warning for un-convertible formula", strings.Join(warnings, "\n"), "x^2 + y^2 = z^2")
				}
			},
		},
		{
			// Regression: LLM output routinely wraps a formula in bold, e.g.
			// "**\(\mathbb{R}^n\)**". Matching "**bold**" before math used to swallow the whole
			// span as a literal-text bold run, so the formula never reached mathRunXML at all
			// and rendered as raw LaTeX source.
			name:    "bold-wrapped math still converts",
			content: "Point 1\nSee **\\(q^4\\)** for details",
			verify: func(t *testing.T, contentSlide string, warnings []string) {
				t.Helper()
				if hasPandoc(t) {
					// Before the fix, "**bold**" matched first and swallowed the whole span as
					// a literal bold text run — no mc:AlternateContent/m:oMath would appear.
					mustContainAll(t, "bold-wrapped math with pandoc available", contentSlide, "mc:AlternateContent", "m:oMath", "m:sSup")
					if len(warnings) != 0 {
						t.Fatalf("expected no warnings on successful conversion, got: %v", warnings)
					}
				} else {
					mustContainAll(t, "bold-wrapped math without pandoc", contentSlide, "q^4")
				}
			},
		},
		{
			// Regression: "\( x^2 \)" with padding space right after "\(" and before "\)" (the
			// LLM writes both padded and unpadded delimiters) used to silently fail: pandoc's
			// markdown reader requires a non-space char immediately inside "$...$" to recognize
			// inline math at all (disambiguating from currency like "$5"), so wrapping the
			// untrimmed, space-padded formula in "$...$" made pandoc treat the whole thing as
			// literal text — exit 0, no error, just no <m:oMath> either.
			name:    "padded delimiters still convert",
			content: "Point 1\nSee \\( y^6 + z^6 = w^6 \\) for details",
			verify: func(t *testing.T, contentSlide string, warnings []string) {
				t.Helper()
				if hasPandoc(t) {
					mustContainAll(t, "padded-delimiter math with pandoc available", contentSlide, "mc:AlternateContent", "m:oMath", "m:sSup")
					if len(warnings) != 0 {
						t.Fatalf("expected no warnings on successful conversion, got: %v", warnings)
					}
				} else {
					mustContainAll(t, "padded-delimiter math without pandoc", contentSlide, "y^6 + z^6 = w^6")
				}
			},
		},
		{
			// Rendering a formula when the converter can't find pandoc at all must degrade to
			// plain escaped text, not error — and, per this PR, must report a warning instead of
			// the previously entirely-silent fallback.
			name:         "pandoc unavailable falls back gracefully",
			content:      "See \\(n^3\\) here",
			pathOverride: true,
			verify: func(t *testing.T, contentSlide string, warnings []string) {
				t.Helper()
				mustContainAll(t, "content slide with no pandoc on PATH", contentSlide, "n^3")
				if strings.Contains(contentSlide, "mc:AlternateContent") {
					t.Fatalf("expected no math markup with empty PATH, got: %s", contentSlide)
				}
				mustContainAll(t, "warning for missing pandoc", strings.Join(warnings, "\n"), "n^3")
			},
		},
		{
			name:         "pandoc present but exits nonzero",
			content:      "Point 1\nSee \\(m^7\\) for details",
			pathOverride: true,
			pandocScript: "#!/bin/sh\nexit 1\n",
			verify:       verifyMathFallback,
		},
		{
			name:         "pandoc emits non-zip garbage",
			content:      "Point 1\nSee \\(m^7\\) for details",
			pathOverride: true,
			pandocScript: "#!/bin/sh\nprintf 'not a zip file'\n",
			verify:       verifyMathFallback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.pathOverride {
				t.Parallel()
			} else {
				// Not t.Parallel(): stubPandoc mutates PATH via t.Setenv.
				stubPandoc(t, tt.pandocScript)
			}
			contentSlide, warnings := renderMathContent(t, tt.content)
			tt.verify(t, contentSlide, warnings)
		})
	}

	// The remaining two "pandoc emits a superficially-valid-but-unusable docx" cases build their
	// script content from a real zip byte buffer, which needs its own t (for t.Fatal on zip
	// construction failure) rather than fitting the simple string-literal table shape above.
	t.Run("pandoc emits valid zip with no word/document.xml", func(t *testing.T) {
		// Not t.Parallel(): stubPandoc mutates PATH via t.Setenv.
		z := zipBytes(t, map[string]string{"other.xml": "<root/>"})
		stubPandoc(t, catScript(t, z))
		contentSlide, warnings := renderMathContent(t, "Point 1\nSee \\(m^7\\) for details")
		verifyMathFallback(t, contentSlide, warnings)
	})
	t.Run("pandoc emits valid docx with no m:oMath in document.xml", func(t *testing.T) {
		// Not t.Parallel(): stubPandoc mutates PATH via t.Setenv.
		z := zipBytes(t, map[string]string{"word/document.xml": "<w:document><w:body>plain</w:body></w:document>"})
		stubPandoc(t, catScript(t, z))
		contentSlide, warnings := renderMathContent(t, "Point 1\nSee \\(m^7\\) for details")
		verifyMathFallback(t, contentSlide, warnings)
	})
	t.Run("pandoc emits docx whose document.xml fails checksum on read", func(t *testing.T) {
		// Not t.Parallel(): stubPandoc mutates PATH via t.Setenv.
		// A syntactically-valid zip whose word/document.xml entry fails its CRC-32 checksum on
		// read — distinct from "non-zip garbage" above, which fails earlier at zip.NewReader
		// itself.
		z := zipBytesWithCorruptEntry(
			map[string][]byte{"word/document.xml": []byte("<w:document><w:body>plain</w:body></w:document>")},
			"word/document.xml",
		)
		stubPandoc(t, catScript(t, z))
		contentSlide, warnings := renderMathContent(t, "Point 1\nSee \\(m^7\\) for details")
		verifyMathFallback(t, contentSlide, warnings)
	})
}

// verifyMathFallback asserts formula "m^7" (the shared formula for every case in this file that
// exercises a fallback path) appears as plain escaped text, no math markup was produced, and a
// warning names the failed formula — the expected outcome whenever pandoc is present but its
// output can't be turned into usable OMML.
func verifyMathFallback(t *testing.T, contentSlide string, warnings []string) {
	t.Helper()
	mustContainAll(t, "fallback text", contentSlide, "m^7")
	if strings.Contains(contentSlide, "mc:AlternateContent") {
		t.Fatalf("expected graceful fallback (no math markup), got: %s", contentSlide)
	}
	mustContainAll(t, "warning for un-convertible formula", strings.Join(warnings, "\n"), "m^7")
}

// TestToOMML_CacheHit is a justified exception to one-function-per-scenario consolidation above:
// it needs real pandoc (gated by hasPandoc, no PATH override) and asserts a distinct property
// (repeat-formula caching) that doesn't fit the pass/fail verify shape used elsewhere in this
// file.
func TestToOMML_CacheHit(t *testing.T) {
	t.Parallel()
	if !hasPandoc(t) {
		t.Skip("pandoc not available on PATH")
	}

	// Same formula twice: the second occurrence must hit toOMML's cache instead of re-invoking
	// pandoc, and still render identical math markup.
	contentSlide, warnings := renderMathContent(t, "Point 1\nSee \\(k^5\\) and again \\(k^5\\) for details")
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings on successful conversion, got: %v", warnings)
	}
	if got := strings.Count(contentSlide, "m:oMath"); got < 2 {
		t.Fatalf("expected repeated formula to render twice (cache hit reuses the same OMML), got %d occurrences: %s", got, contentSlide)
	}
}
