package commands_test

import (
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

// slidesTestCases builds TestSlidesCmdRun_Table's table, including resolveSlideRange's
// auto-scaling and clamp behavior (resolveSlideRange itself is unexported, and this package
// tests commands black-box, so it's exercised indirectly here) via the "auto range" cases below —
// asserting only that each min/max combination succeeds and produces output, not an exact slide
// count: the stub section-outline LLM (stubSectionOutlineJSON) returns a fixed entry count
// regardless of any requested range (per-section planning has no numeric target to hit at all,
// see generateSectionOutline's doc comment), so an exact count is no longer a meaningful signal
// at this layer. AutoSlideRange's own arithmetic is precisely covered by TestAutoSlideRange in
// internal/distill. Split out from the test function itself to keep gocyclo's complexity count on
// the (trivial) runner, not this literal.
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
			// calling the LLM. Both are explicit/nonzero, so resolveSlideRange's auto-scaling
			// doesn't apply here — see the "auto range" cases below for that.
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
		{
			name:       "auto range: both unset gets full auto",
			prepare:    slidesAutoScalingPrepare(0, 0),
			outputFile: "src01_slides.md",
		},
		{
			name:       "auto range: min explicit within auto max passes through, auto max unaffected",
			prepare:    slidesAutoScalingPrepare(10, 0),
			outputFile: "src01_slides.md",
		},
		{
			name:       "auto range: max explicit above auto min passes through, auto min unaffected",
			prepare:    slidesAutoScalingPrepare(0, 30),
			outputFile: "src01_slides.md",
		},
		{
			// Before the clamp fix, this errored ("invalid slide range") because the auto max
			// (27) landed below the explicit min.
			name:       "auto range: min explicit exceeds auto max, auto max clamped up to min",
			prepare:    slidesAutoScalingPrepare(30, 0),
			outputFile: "src01_slides.md",
		},
		{
			// Before the clamp fix, this errored ("invalid slide range") because the auto min
			// (19) landed above the explicit max.
			name:       "auto range: max explicit below auto min, auto min clamped down to max",
			prepare:    slidesAutoScalingPrepare(0, 15),
			outputFile: "src01_slides.md",
		},
		{
			name:       "auto range: both explicit, no auto-scaling involved",
			prepare:    slidesAutoScalingPrepare(3, 8),
			outputFile: "src01_slides.md",
		},
		{
			// A chapter with no Sections falls back to one synthesized pseudo-section
			// (generateOutlineChunked) rather than failing — exercised end-to-end here.
			name:       "zero sections falls back to one pseudo-section",
			prepare:    slidesZeroSectionsPrepare,
			outputFile: "src01_slides.md",
		},
		{
			// Proves DistilledContext.Sections -> ProtoChapterInput.Sections wiring at the
			// command layer, not just inside the distill package's own tests.
			name:       "multiple sections wired through to chunked outline generation",
			prepare:    slidesMultiSectionsPrepare,
			outputFile: "src01_slides.md",
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
			err := cmd.Run(t.Context(), cli)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			assertSlidesOutput(t, dir, tt.outputFile, tt.wantErr)
		})
	}
}
