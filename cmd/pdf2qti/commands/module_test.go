package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

type moduleTestCase struct {
	name           string
	prepare        func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI)
	wantErr        bool
	checkFile      bool
	wantSlideCount int // when > 0 (and checkFile), mod1_module.md's total slide count must equal this
}

// moduleTestCases builds TestModuleCmdRun_Table's table, including resolveSlideRange's
// auto-scaling and clamp behavior (resolveSlideRange itself is unexported, and this package
// tests commands black-box, so it's exercised indirectly here). AutoSlideRange sums Text length
// across all of a module's chapters; the auto-scaling cases below use two 4000-char chapter
// texts (src01 + src02) summing to the same 8000 chars as the slides test, giving the same known
// auto range: minSlides=19, maxSlides=27. Split out from the test function itself to keep
// gocyclo's complexity count on the (trivial) runner, not this literal.
func moduleTestCases() []moduleTestCase {
	return []moduleTestCase{
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
		{
			// Pre-creating a directory at the module JSON output path makes SaveModuleDoc's
			// os.WriteFile fail.
			name: "save module doc error, json output path is a directory",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				writeDistilledContextFileWithID(t, dir, "src02")
				if err := os.Mkdir(filepath.Join(dir, "mod1_module.json"), 0o750); err != nil {
					t.Fatal(err)
				}
				return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			// Pre-creating a directory at the module Markdown output path makes
			// SaveModuleMarkdown's os.WriteFile fail.
			name: "save module markdown error, md output path is a directory",
			prepare: func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeModuleConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				writeDistilledContextFileWithID(t, dir, "src02")
				if err := os.Mkdir(filepath.Join(dir, "mod1_module.md"), 0o750); err != nil {
					t.Fatal(err)
				}
				return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name:           "auto range: both unset gets full auto",
			prepare:        moduleAutoScalingPrepare(0, 0),
			checkFile:      true,
			wantSlideCount: autoMin,
		},
		{
			// Before the clamp fix, this errored ("invalid slide range") because the auto max
			// (27) landed below the explicit min.
			name:           "auto range: min explicit exceeds auto max, auto max clamped up to min",
			prepare:        moduleAutoScalingPrepare(30, 0),
			checkFile:      true,
			wantSlideCount: 30,
		},
		{
			// Before the clamp fix, this errored ("invalid slide range") because the auto min
			// (19) landed above the explicit max.
			name:           "auto range: max explicit below auto min, auto min clamped down to max",
			prepare:        moduleAutoScalingPrepare(0, 15),
			checkFile:      true,
			wantSlideCount: 15,
		},
	}
}

func TestModuleCmdRun_Table(t *testing.T) {
	t.Parallel()

	for _, tt := range moduleTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd, cli := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), cli)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.checkFile {
				mdPath := filepath.Join(dir, "mod1_module.md")
				if _, statErr := os.Stat(mdPath); statErr != nil {
					t.Fatalf("expected module markdown output: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(dir, "mod1_module.json")); statErr != nil {
					t.Fatalf("expected module json output: %v", statErr)
				}
				assertSlideCount(t, mdPath, tt.wantSlideCount)
			}
		})
	}
}
