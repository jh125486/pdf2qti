package commands_test

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestApproveCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) string
		wantErr bool
	}{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte(validQuizMD), 0o600); err != nil {
					t.Fatal(err)
				}
				return cfgFile
			},
		},
		{
			name:    "load config error",
			prepare: func(_ *testing.T, _ string) string { return "nonexistent_config.json" },
			wantErr: true,
		},
		{
			name: "source error missing quiz",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				return cfgFile
			},
			wantErr: true,
		},
		{
			// render.ParseDraft never itself returns an error (any markdown parses, even if it
			// yields zero questions), but qti.BuildAssessment requires a non-empty title — a
			// draft with no leading "# Title" heading is the one reachable way to make
			// runApproveSource's "build assessment" error wrap trigger.
			name: "build assessment error missing title",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				noTitleMD := "## MC\n\n1. What is 2+2?\n[ ] 3\n[*] 4\n[ ] 5\n"
				if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte(noTitleMD), 0o600); err != nil {
					t.Fatal(err)
				}
				return cfgFile
			},
			wantErr: true,
		},
		{
			// Pre-creating a directory at package output path makes os.OpenFile fail.
			name: "create QTI package error, output path is a directory",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte(validQuizMD), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "src01.zip"), 0o750); err != nil {
					t.Fatal(err)
				}
				return cfgFile
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfgFile := tt.prepare(t, dir)
			cmd := &commands.ApproveCmd{}
			err := cmd.Run(context.Background(), &commands.CLI{Config: cfgFile})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertCanvasPackage(t, filepath.Join(dir, "src01.zip"))
			}
		})
	}
}

func assertCanvasPackage(t *testing.T, path string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open QTI package: %v", err)
	}
	defer zr.Close()
	if len(zr.File) != 2 || zr.File[0].Name != "imsmanifest.xml" || zr.File[1].Name != "src01.xml" {
		t.Fatalf("unexpected QTI package entries: %+v", zr.File)
	}
	r, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if len(manifest) == 0 {
		t.Fatal("empty package manifest")
	}
}
