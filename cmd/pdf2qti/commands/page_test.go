package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestPageCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, dir string) commands.PageCmd
		wantErr   bool
		checkFile bool
	}{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) commands.PageCmd {
				t.Helper()
				writeDistilledContextFile(t, dir)
				templatePath := filepath.Join(dir, "page.tmpl")
				outPath := filepath.Join(dir, "out.html")
				if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.PageCmd{Context: filepath.Join(dir, "src01_context.json"), Template: templatePath, Output: outPath}
			},
			checkFile: true,
		},
		{
			name: "create output error",
			prepare: func(t *testing.T, dir string) commands.PageCmd {
				t.Helper()
				writeDistilledContextFile(t, dir)
				templatePath := filepath.Join(dir, "page.tmpl")
				if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.PageCmd{Context: filepath.Join(dir, "src01_context.json"), Template: templatePath, Output: dir}
			},
			wantErr: true,
		},
		{
			name: "load context error",
			prepare: func(_ *testing.T, _ string) commands.PageCmd {
				return commands.PageCmd{Context: "/no/context.json", Template: "/no/template.tmpl"}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), &commands.CLI{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.checkFile {
				if _, statErr := os.Stat(filepath.Join(dir, "out.html")); statErr != nil {
					t.Fatalf("expected output file: %v", statErr)
				}
			}
		})
	}
}

func TestExecute_PageCommandSuccess(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir)
	templatePath := filepath.Join(dir, "page.tmpl")
	outPath := filepath.Join(dir, "out.html")
	if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	withArgs(t, []string{
		"pdf2qti",
		"--config", filepath.Join(dir, "unused.json"),
		"page",
		"--context", filepath.Join(dir, "src01_context.json"),
		"--output", outPath,
		templatePath,
	})

	if err := commands.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
}
