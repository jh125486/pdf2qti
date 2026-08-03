package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestModuleCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI)
		wantErr   bool
		checkFile bool
	}{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				writeDistilledContextFileWithID(t, dir, "src02")
				return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			checkFile: true,
		},
		{
			name: "unknown module id",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				return commands.ModuleCmd{ID: "nope", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "missing chapter context",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "already exists without force",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				writeDistilledContextFileWithID(t, dir, "src02")
				if err := os.WriteFile(filepath.Join(dir, "mod1_module.md"), []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "force overwrites existing module doc",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				writeDistilledContextFileWithID(t, dir, "src02")
				if err := os.WriteFile(filepath.Join(dir, "mod1_module.md"), []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.ModuleCmd{ID: "mod1", Force: true, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			checkFile: true,
		},
		{
			name: "load config error",
			prepare: func(_ *testing.T, _ string) (commands.ModuleCmd, *commands.CLI) {
				return commands.ModuleCmd{ID: "mod1"}, &commands.CLI{Config: "nonexistent_config.json"}
			},
			wantErr: true,
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
				if _, statErr := os.Stat(filepath.Join(dir, "mod1_module.md")); statErr != nil {
					t.Fatalf("expected module markdown output: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(dir, "mod1_module.json")); statErr != nil {
					t.Fatalf("expected module json output: %v", statErr)
				}
			}
		})
	}
}

// TestModuleCmdRun_AutoSlideRange is module.go's counterpart to
// TestSlidesCmdRun_AutoSlideRange: it exercises resolveSlideRange's auto-scaling and clamp
// behavior through ModuleCmd.Run, since AutoSlideRange sums Text length across all of a module's
// chapters. Two 4000-char chapter texts (src01 + src02) sum to the same 8000 chars used in the
// slides test, giving the same known auto range: minSlides=19, maxSlides=27.
func TestModuleCmdRun_AutoSlideRange(t *testing.T) {
	t.Parallel()

	const (
		autoMin = 19
		autoMax = 27
	)

	tests := []struct {
		name       string
		minSlides  int
		maxSlides  int
		wantSlides int
	}{
		{name: "both unset: full auto", minSlides: 0, maxSlides: 0, wantSlides: autoMin},
		{name: "min explicit exceeds auto max: auto max clamped up to min", minSlides: 30, maxSlides: 0, wantSlides: 30},
		{name: "max explicit below auto min: auto min clamped down to max", minSlides: 0, maxSlides: 15, wantSlides: 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pdfPath := filepath.Join(dir, "src.pdf")
			if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfgPath := writeModuleConfigFile(t, dir, pdfPath)
			writeDistilledContextFileWithText(t, dir, "src01", strings.Repeat("x", 4000))
			writeDistilledContextFileWithText(t, dir, "src02", strings.Repeat("x", 4000))

			cmd := commands.ModuleCmd{ID: "mod1", MinSlides: tt.minSlides, MaxSlides: tt.maxSlides}
			cli := &commands.CLI{Config: cfgPath}
			if err := cmd.Run(context.Background(), cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			outFile := filepath.Join(dir, "mod1_module.md")
			if got := countSlideMetaMarkers(t, outFile); got != tt.wantSlides {
				t.Fatalf("slide count = %d, want %d", got, tt.wantSlides)
			}
		})
	}
}
