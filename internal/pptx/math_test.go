package pptx_test

import (
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

func TestRender_InlineMathInBullet(t *testing.T) {
	t.Parallel()

	dc := sampleContext()
	dc.Slides[0].Content = "Point 1\nSee \\(x^2 + y^2 = z^2\\) for details"

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, baseTemplateEntries()); err != nil {
		t.Fatal(err)
	}

	if err := pptx.Render(templatePath, dc, "", nil, outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	contentSlide := string(outEntries["ppt/slides/slide2.xml"])

	if hasPandoc(t) {
		mustContainAll(t, "content slide with pandoc available", contentSlide,
			"mc:AlternateContent", "m:oMath", "m:sSup")
		// The escaped LaTeX must still be present as the mc:Fallback for older PowerPoint.
		if !strings.Contains(contentSlide, "mc:Fallback") {
			t.Fatalf("expected mc:Fallback alongside real math, got: %s", contentSlide)
		}
	} else {
		// No pandoc: must degrade to plain escaped text, never a hard error or dropped content.
		mustContainAll(t, "content slide without pandoc", contentSlide, "x^2 + y^2 = z^2")
		if strings.Contains(contentSlide, "mc:AlternateContent") {
			t.Fatalf("expected no math markup without pandoc, got: %s", contentSlide)
		}
	}
}

func TestRender_BoldWrappedMathStillConverts(t *testing.T) {
	t.Parallel()

	// Regression: LLM output routinely wraps a formula in bold, e.g. "**\(\mathbb{R}^n\)**".
	// Matching "**bold**" before math used to swallow the whole span as a literal-text bold run,
	// so the formula never reached mathRunXML at all and rendered as raw LaTeX source.
	dc := sampleContext()
	dc.Slides[0].Content = "Point 1\nSee **\\(x^2\\)** for details"

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, baseTemplateEntries()); err != nil {
		t.Fatal(err)
	}

	if err := pptx.Render(templatePath, dc, "", nil, outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	contentSlide := string(outEntries["ppt/slides/slide2.xml"])

	if hasPandoc(t) {
		// Before the fix, "**bold**" matched first and swallowed the whole span as a literal
		// bold text run — no mc:AlternateContent/m:oMath would appear at all in that case.
		mustContainAll(t, "bold-wrapped math with pandoc available", contentSlide, "mc:AlternateContent", "m:oMath", "m:sSup")
	} else {
		mustContainAll(t, "bold-wrapped math without pandoc", contentSlide, "x^2")
	}
}

func TestRender_PaddedDelimitersStillConvert(t *testing.T) {
	t.Parallel()

	// Regression: "\( x^2 \)" with padding space right after "\(" and before "\)" (the LLM
	// writes both padded and unpadded delimiters) used to silently fail: pandoc's markdown
	// reader requires a non-space char immediately inside "$...$" to recognize inline math at
	// all (disambiguating from currency like "$5"), so wrapping the untrimmed, space-padded
	// formula in "$...$" made pandoc treat the whole thing as literal text — exit 0, no error,
	// just no <m:oMath> either, so it looked like a silent no-op rather than a clear failure.
	dc := sampleContext()
	dc.Slides[0].Content = "Point 1\nSee \\( x^2 + y^2 = z^2 \\) for details"

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, baseTemplateEntries()); err != nil {
		t.Fatal(err)
	}

	if err := pptx.Render(templatePath, dc, "", nil, outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	contentSlide := string(outEntries["ppt/slides/slide2.xml"])

	if hasPandoc(t) {
		mustContainAll(t, "padded-delimiter math with pandoc available", contentSlide, "mc:AlternateContent", "m:oMath", "m:sSup")
	} else {
		mustContainAll(t, "padded-delimiter math without pandoc", contentSlide, "x^2 + y^2 = z^2")
	}
}

func TestMathConverter_UnavailableFallsBackGracefully(t *testing.T) {
	// No t.Parallel(): t.Setenv panics in parallel tests, and this test needs to control PATH.

	// Rendering a formula when the converter can't find pandoc must degrade to plain escaped
	// text, not error — exercised indirectly via a real Render call with an empty PATH, since
	// the converter type and its cache are unexported.
	dc := sampleContext()
	dc.Slides[0].Content = "See \\(x^2\\) here"

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, baseTemplateEntries()); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir) // a directory with no pandoc binary in it
	if err := pptx.Render(templatePath, dc, "", nil, outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	contentSlide := string(outEntries["ppt/slides/slide2.xml"])
	mustContainAll(t, "content slide with no pandoc on PATH", contentSlide, "x^2")
	if strings.Contains(contentSlide, "mc:AlternateContent") {
		t.Fatalf("expected no math markup with empty PATH, got: %s", contentSlide)
	}
}
