package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestSlidesCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prepare    func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI)
		wantErr    bool
		outputFile string // relative to dir; checked to exist when non-empty and !wantErr
	}{
		{
			name: "success by id",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			outputFile: "src01_slides.md",
		},
		{
			name: "success with --all",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				return commands.SlidesCmd{All: true, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			outputFile: "src01_slides.md",
		},
		{
			name: "custom output path",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				out := filepath.Join(dir, "custom_deck.md")
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8, Output: out}, &commands.CLI{Config: cfgPath}
			},
			outputFile: "custom_deck.md",
		},
		{
			name: "no sources selected",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.SlidesCmd{MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "ids selected but none match",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.SlidesCmd{IDs: []string{"nope"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "missing chapter context",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "already exists without force",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				if err := os.WriteFile(filepath.Join(dir, "src01_slides.md"), []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "force overwrites existing output",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				if err := os.WriteFile(filepath.Join(dir, "src01_slides.md"), []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.SlidesCmd{IDs: []string{"src01"}, Force: true, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			outputFile: "src01_slides.md",
		},
		{
			name: "load config error",
			prepare: func(_ *testing.T, _ string) (commands.SlidesCmd, *commands.CLI) {
				return commands.SlidesCmd{IDs: []string{"src01"}}, &commands.CLI{Config: "nonexistent_config.json"}
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
			if tt.outputFile != "" {
				if _, statErr := os.Stat(filepath.Join(dir, tt.outputFile)); statErr != nil {
					t.Fatalf("expected slides output %q: %v", tt.outputFile, statErr)
				}
			}
		})
	}
}

// TestSlidesCmdRun_AutoSlideRange exercises resolveSlideRange's auto-scaling and clamp behavior
// through the public SlidesCmd.Run API (resolveSlideRange itself is unexported, and this package
// tests commands black-box). The stub outline LLM (stubOutlineJSON) always emits exactly the
// requested minimum content-slide count, so the resulting file's slide count == the resolved
// minSlides — letting these cases assert resolveSlideRange's output indirectly but precisely.
//
// The fixture chapter's Text is 8000 chars (20 * charsPerContentSlide), giving a known
// auto-computed range of minSlides=19, maxSlides=27 (see distill.AutoSlideRange's -15%/+25%
// band and +2 for agenda/summary) to test against.
func TestSlidesCmdRun_AutoSlideRange(t *testing.T) {
	t.Parallel()

	const (
		autoMin = 19
		autoMax = 27
	)

	tests := []struct {
		name         string
		minSlides    int
		maxSlides    int
		wantSlides   int
		wantErrOnOld bool // documents that this case errored ("invalid slide range") before the clamp fix
	}{
		{name: "both unset: full auto", minSlides: 0, maxSlides: 0, wantSlides: autoMin},
		{name: "min explicit within auto max: passthrough min, auto max unaffected", minSlides: 10, maxSlides: 0, wantSlides: 10},
		{name: "max explicit above auto min: auto min unaffected, passthrough max", minSlides: 0, maxSlides: 30, wantSlides: autoMin},
		{name: "min explicit exceeds auto max: auto max clamped up to min", minSlides: 30, maxSlides: 0, wantSlides: 30, wantErrOnOld: true},
		{name: "max explicit below auto min: auto min clamped down to max", minSlides: 0, maxSlides: 15, wantSlides: 15, wantErrOnOld: true},
		{name: "both explicit: no auto-scaling involved", minSlides: 3, maxSlides: 8, wantSlides: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			pdfPath := filepath.Join(dir, "src.pdf")
			if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfgPath := writeConfigFile(t, dir, pdfPath)
			writeDistilledContextFileWithText(t, dir, "src01", strings.Repeat("x", 8000))

			cmd := commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: tt.minSlides, MaxSlides: tt.maxSlides}
			cli := &commands.CLI{Config: cfgPath}
			if err := cmd.Run(context.Background(), cli); err != nil {
				t.Fatalf("unexpected error (wantErrOnOld=%v documents pre-fix behavior only): %v", tt.wantErrOnOld, err)
			}

			outFile := filepath.Join(dir, "src01_slides.md")
			if got := countSlideMetaMarkers(t, outFile); got != tt.wantSlides {
				t.Fatalf("slide count = %d, want %d", got, tt.wantSlides)
			}
		})
	}
}
