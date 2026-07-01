package page_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/page"
)

func TestRender_Table(t *testing.T) {
	t.Parallel()

	dc := &distill.DistilledContext{
		SourceID:         "src01",
		Book:             "TLPI",
		Chapter:          21,
		ModuleName:       "Signals",
		Overview:         "<p>Overview</p>",
		KeyConcepts:      []string{"SIGINT", "Handlers"},
		MaterialOverview: "Read chapter",
		TeachingNotes:    "Focus on signal-safe functions",
	}

	tests := []struct {
		name       string
		template   string
		vars       map[string]string
		wantErr    bool
		errLike    string
		wantTokens []string
	}{
		{
			name:     "success",
			template: "{{.module_name}}|{{.book}}|{{.chapter}}|{{.material_overview}}",
			vars:     map[string]string{"module_name": "Signals Module"},
			wantTokens: []string{
				"Signals Module",
				"TLPI",
				"21",
				"Read chapter",
			},
		},
		{
			name:     "template parse error",
			template: "{{.module_name",
			vars:     map[string]string{},
			wantErr:  true,
			errLike:  "parse template",
		},
		{
			name:    "missing template file",
			wantErr: true,
			errLike: "read template",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			templatePath := filepath.Join(t.TempDir(), "page.html.tmpl")
			if tt.name == "missing template file" {
				templatePath = filepath.Join(t.TempDir(), "missing.html.tmpl")
			} else {
				if err := os.WriteFile(templatePath, []byte(tt.template), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			var out bytes.Buffer
			err := page.Render(templatePath, dc, tt.vars, &out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			for _, tok := range tt.wantTokens {
				if !strings.Contains(out.String(), tok) {
					t.Fatalf("expected output to contain %q, got %q", tok, out.String())
				}
			}
		})
	}
}
