package commands_test

import (
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

// TestExecute_Table exercises commands.Execute directly by manipulating the
// process-global os.Args. Only scenarios that fail *after* kong successfully
// parses the command line are safe to test in-process: kong.Parse calls
// os.Exit(1) internally on parse errors (no kong.Exit override is configured
// in Execute), which would kill the whole test binary. Parse-error scenarios
// (e.g. unknown subcommands) are covered out-of-process in main_test.go.
func TestExecute_Table(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) []string
		wantErr bool
	}{
		{
			name: "missing config file",
			prepare: func(_ *testing.T, dir string) []string {
				return []string{"pdf2qti", "-c", filepath.Join(dir, "does-not-exist.json"), "validate"}
			},
			wantErr: true,
		},
		{
			name: "validate success",
			prepare: func(t *testing.T, dir string) []string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte(validQuizMD), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"pdf2qti", "-c", cfgFile, "validate"}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			args := tt.prepare(t, dir)

			origArgs := os.Args
			os.Args = args
			t.Cleanup(func() { os.Args = origArgs })

			err := commands.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
