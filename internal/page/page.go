// Package page provides HTML page rendering from distilled context and Go templates.
package page

import (
	"fmt"
	"html/template"
	"io"
	"os"

	"github.com/jh125486/pdf2qti/internal/distill"
)

// Render parses a Go HTML template from templatePath, builds a data map from dc
// merged with extra vars, and writes the result to out.
// If out is nil, it writes to os.Stdout.
func Render(templatePath string, dc *distill.DistilledContext, vars map[string]string, out io.Writer) error {
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template %q: %w", templatePath, err)
	}
	tmpl, err := template.New("page").Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parse template %q: %w", templatePath, err)
	}

	data := buildData(dc, vars)

	if out == nil {
		out = os.Stdout
	}
	if err := tmpl.Execute(out, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	return nil
}

// buildData constructs the template data map from a DistilledContext and extra vars.
// Extra vars override context keys if names collide.
func buildData(dc *distill.DistilledContext, vars map[string]string) map[string]any {
	data := map[string]any{
		"source_id":         dc.SourceID,
		"book":              dc.Book,
		"chapter":           dc.Chapter,
		"module_name":       dc.ModuleName,
		"overview":          template.HTML(dc.Overview), //nolint:gosec // Overview is intentionally rendered as HTML.
		"key_concepts":      dc.KeyConcepts,
		"material_overview": dc.MaterialOverview,
		"teaching_notes":    dc.TeachingNotes,
		"objectives":        dc.Objectives,
	}
	for k, v := range vars {
		data[k] = v
	}
	return data
}
