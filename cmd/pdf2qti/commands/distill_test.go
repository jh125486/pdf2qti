package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

type distillTestCase struct {
	name      string
	prepare   func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI)
	wantErr   bool
	checkFile bool
}

// distillTestCases builds TestDistillCmdRun_NoSourcesSelected's table. Split out from the test
// function itself to keep gocyclo's complexity count on the (trivial) runner, not this literal.
func distillTestCases() []distillTestCase {
	return []distillTestCase{
		{
			name: "no sources selected",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf content"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.DistillCmd{}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "success all",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.DistillCmd{All: true}, &commands.CLI{Config: cfgPath}
			},
			checkFile: true,
		},
		{
			name: "success specific ids",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.DistillCmd{IDs: []string{"src01"}}, &commands.CLI{Config: cfgPath}
			},
			checkFile: true,
		},
		{
			name: "ids selected but none match",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.DistillCmd{IDs: []string{"nope"}}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "load config error",
			prepare: func(_ *testing.T, _ string) (commands.DistillCmd, *commands.CLI) {
				return commands.DistillCmd{All: true}, &commands.CLI{Config: "nonexistent_config.json"}
			},
			wantErr: true,
		},
		{
			name: "context already exists without force",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeContextFile(t, dir)
				return commands.DistillCmd{All: true}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "force overwrites existing context",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeContextFile(t, dir)
				return commands.DistillCmd{All: true, Force: true}, &commands.CLI{Config: cfgPath}
			},
			checkFile: true,
		},
		{
			// outDir's parent path component ("blocker") is pre-created as a regular file, so
			// os.MkdirAll(outDir, ...) fails with "not a directory".
			name: "create outDir error",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				blocker := filepath.Join(dir, "blocker")
				if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				outDir := filepath.Join(blocker, "nested")
				cfgPath := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + outDir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.DistillCmd{All: true}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "extract PDF error, source file missing",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				cfgPath := writeConfigFile(t, dir, filepath.Join(dir, "does_not_exist.pdf"))
				return commands.DistillCmd{All: true}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			// Pre-creating a directory at the context output path makes distill.Save's
			// os.WriteFile fail, exercising the "save context" error branch.
			name: "save context error, output path is a directory",
			prepare: func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				if err := os.Mkdir(filepath.Join(dir, "src01_context.json"), 0o750); err != nil {
					t.Fatal(err)
				}
				return commands.DistillCmd{All: true}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
	}
}

func TestDistillCmdRun_NoSourcesSelected(t *testing.T) {
	t.Parallel()

	for _, tt := range distillTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd, cli := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), cli)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.checkFile {
				if _, statErr := os.Stat(filepath.Join(dir, "src01_context.json")); statErr != nil {
					t.Fatalf("expected context output: %v", statErr)
				}
			}
		})
	}
}
