package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestDistillCmdRun_NoSourcesSelected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, dir string) (commands.DistillCmd, *commands.CLI)
		wantErr   bool
		checkFile bool
	}{
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
	}

	for _, tt := range tests {

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
