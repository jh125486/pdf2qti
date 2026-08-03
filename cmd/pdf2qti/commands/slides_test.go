package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

type slidesTestCase struct {
	name       string
	prepare    func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI)
	wantErr    bool
	outputFile string // relative to dir; checked to exist when non-empty and !wantErr
}

// slidesTestCases builds TestSlidesCmdRun_Table's table. Split out from the test function itself
// to keep gocyclo's complexity count on the (trivial) runner, not this literal.
func slidesTestCases() []slidesTestCase {
	return []slidesTestCase{
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
		{
			// MinSlides > MaxSlides makes GenerateProtoDeck reject the range before ever
			// calling the LLM.
			name: "generate proto deck error, invalid slide range",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 10, MaxSlides: 3}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			// Pre-creating a directory at the output path makes os.WriteFile fail.
			name: "write slides error, output path is a directory",
			prepare: func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src.pdf")
				if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFileWithID(t, dir, "src01")
				if err := os.Mkdir(filepath.Join(dir, "src01_slides.md"), 0o750); err != nil {
					t.Fatal(err)
				}
				return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
	}
}

func TestSlidesCmdRun_Table(t *testing.T) {
	t.Parallel()

	for _, tt := range slidesTestCases() {
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
