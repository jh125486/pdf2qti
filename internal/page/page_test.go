package page_test

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/page"
)

func sampleContext() *distill.DistilledContext {
	return &distill.DistilledContext{
		SourceID:         "ch21",
		Book:             "TLPI",
		Chapter:          21,
		ModuleName:       "Module 04: Signals",
		Overview:         "<p>In this module, we examine signals.</p>",
		KeyConcepts:      []string{"SIGINT", "sigaction()", "signal mask"},
		MaterialOverview: "Chapter 21 covers signal handling in Linux.",
		TeachingNotes:    "Emphasize async-signal-safety.",
		Objectives: []distill.Objective{
			{CO: 1, Text: "Write robust software."},
			{CO: 2, Text: "Implement multi-process applications."},
		},
	}
}

func TestRender_LearningObjectives(t *testing.T) {
	dc := sampleContext()
	vars := map[string]string{
		"quiz_title":       "Module 04: Signals",
		"assignment_title": "Module 04: Signal Stopwatch",
	}
	var buf bytes.Buffer
	err := page.Render("testdata/learning_objectives.html.tmpl", dc, vars, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Write robust software.") {
		t.Error("expected objective text in output")
	}
	if !strings.Contains(out, "(CO 1)") {
		t.Error("expected CO number in output")
	}
	if !strings.Contains(out, "Module 04: Signals") {
		t.Error("expected quiz_title in output")
	}
	if !strings.Contains(out, "Module 04: Signal Stopwatch") {
		t.Error("expected assignment_title in output")
	}
}

func TestRender_Materials(t *testing.T) {
	dc := sampleContext()
	var buf bytes.Buffer
	err := page.Render("testdata/materials.html.tmpl", dc, nil, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "SIGINT") {
		t.Error("expected key concept SIGINT in output")
	}
	if !strings.Contains(out, "TLPI") {
		t.Error("expected book name in output")
	}
	if !strings.Contains(out, "Chapter 21") {
		t.Error("expected chapter number in output")
	}
}

func TestRender_OverviewNotEscaped(t *testing.T) {
	dc := sampleContext()
	dc.Overview = "<p>We examine <code>SIGINT</code>.</p>"
	tmplPath := filepath.Join(t.TempDir(), "tmpl.html")
	if err := os.WriteFile(tmplPath, []byte(`{{.overview}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := page.Render(tmplPath, dc, nil, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	// overview is template.HTML — must not be escaped
	if strings.Contains(out, "&lt;p&gt;") {
		t.Error("overview HTML must not be escaped")
	}
	if !strings.Contains(out, "<p>We examine <code>SIGINT</code>.</p>") {
		t.Errorf("expected raw HTML in output, got: %s", out)
	}
}

func TestRender_BadTemplateSyntax(t *testing.T) {
	tmplPath := filepath.Join(t.TempDir(), "bad.html")
	if err := os.WriteFile(tmplPath, []byte(`{{.foo`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := page.Render(tmplPath, sampleContext(), nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for bad template syntax")
	}
	if !strings.Contains(err.Error(), "parse template") {
		t.Errorf("expected 'parse template' in error, got: %v", err)
	}
}

func TestRender_MissingTemplateFile(t *testing.T) {
	err := page.Render("/no/such/template.html", sampleContext(), nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for missing template file")
	}
	if !strings.Contains(err.Error(), "read template") {
		t.Errorf("expected 'read template' in error, got: %v", err)
	}
}

func TestRender_ToFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output.html")
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dc := sampleContext()
	if err := page.Render("testdata/materials.html.tmpl", dc, nil, f); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.Close()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "SIGINT") {
		t.Error("expected SIGINT in output file")
	}
}

func TestRender_VarsOverrideContext(t *testing.T) {
	dc := sampleContext()
	// Override the module_name via vars
	vars := map[string]string{"module_name": "Custom Name"}
	tmplPath := filepath.Join(t.TempDir(), "tmpl.html")
	if err := os.WriteFile(tmplPath, []byte(`{{.module_name}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := page.Render(tmplPath, dc, vars, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "Custom Name" {
		t.Errorf("expected vars to override context, got: %s", buf.String())
	}
}

func TestRender_VarsAreHTMLEscaped(t *testing.T) {
	dc := sampleContext()
	vars := map[string]string{"unsafe": "<script>alert(1)</script>"}
	tmplPath := filepath.Join(t.TempDir(), "tmpl.html")
	if err := os.WriteFile(tmplPath, []byte(`{{.unsafe}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := page.Render(tmplPath, dc, vars, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>") {
		t.Error("vars must be HTML-escaped; got raw <script> tag")
	}
}

func TestRender_NilWriterUsesStdout(t *testing.T) {
	// We can't easily capture stdout, so just ensure no panic/error
	dc := sampleContext()
	// Use a template that writes nothing to avoid stdout noise
	tmplPath := filepath.Join(t.TempDir(), "empty.html")
	if err := os.WriteFile(tmplPath, []byte(``), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(tmplPath, dc, nil, nil); err != nil {
		t.Fatalf("unexpected error with nil writer: %v", err)
	}
}

// TestBuildData_OverviewIsTemplateHTML ensures overview is stored as template.HTML.
func TestBuildData_OverviewIsTemplateHTML(t *testing.T) {
	dc := sampleContext()
	vars := map[string]string{}
	tmplPath := filepath.Join(t.TempDir(), "tmpl.html")
	// Template that directly renders overview — must NOT be escaped
	if err := os.WriteFile(tmplPath, []byte(`{{.overview}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := page.Render(tmplPath, dc, vars, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// If overview were a plain string, html/template would escape <p>
	if strings.Contains(buf.String(), "&lt;p&gt;") {
		t.Errorf("overview must be template.HTML, got escaped output: %s", buf.String())
	}
}

// Ensure template.HTML type is accessible (compile check)
var _ template.HTML = template.HTML("")
