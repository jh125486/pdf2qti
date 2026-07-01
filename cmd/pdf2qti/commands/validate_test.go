package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestValidateCmdRun_Table(t *testing.T) {
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
			name: "validation fails",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"},"validation":{"requireSequentialNumbering":true}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte(validationFailQuizMD), 0o600); err != nil {
					t.Fatal(err)
				}
				return cfgFile
			},
			wantErr: true,
		},
		{
			name:    "load config error",
			prepare: func(_ *testing.T, _ string) string { return "nonexistent_config.json" },
			wantErr: true,
		},
		{
			name: "source error",
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
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfgFile := tt.prepare(t, dir)
			cmd := &commands.ValidateCmd{}
			err := cmd.Run(context.Background(), &commands.CLI{Config: cfgFile})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
