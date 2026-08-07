package commands_test

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
	"github.com/jh125486/pdf2qti/internal/audit"
)

const realPPTXTemplate = "../../../internal/pptx/testdata/template.pptx"

// writePPTXConfigFile writes a minimal valid config with the given courseName and returns its
// path. PPTXCmd loads config for CourseName alone, so the config doesn't need real sources.
func writePPTXConfigFile(t *testing.T, dir, courseName string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "quiz.json")
	cfgJSON := fmt.Sprintf(`{"version":1,"courseName":%q,"sources":[{"id":"src01","pdf":"unused.pdf"}]}`, courseName)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

type pptxCmdTestCase struct {
	name    string
	prepare func(t *testing.T, dir string) commands.PPTXCmd
	wantErr bool
	verify  func(t *testing.T, dir string)
}

// pptxCmdTestCases builds TestPPTXCmdRun_Table's table. Split out from the test function itself
// to keep gocyclo's complexity count on the (trivial) runner, not this literal.
func pptxCmdTestCases() []pptxCmdTestCase {
	return []pptxCmdTestCase{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) commands.PPTXCmd {
				t.Helper()
				slidesPath := writeSlidesMarkdownFile(t, dir)
				outPath := filepath.Join(dir, "out.pptx")
				return commands.PPTXCmd{Slides: slidesPath, Template: realPPTXTemplate, Output: outPath}
			},
			verify: func(t *testing.T, dir string) {
				t.Helper()
				titleSlide, err := readPPTXEntry(filepath.Join(dir, "out.pptx"), "ppt/slides/slide1.xml")
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"Module 1", "Test University"} {
					if !strings.Contains(string(titleSlide), want) {
						t.Fatalf("expected title slide to contain %q, got: %q", want, string(titleSlide))
					}
				}

				agendaSlide, err := readPPTXEntry(filepath.Join(dir, "out.pptx"), "ppt/slides/slide2.xml")
				if err != nil {
					t.Fatal(err)
				}
				for _, bullet := range []string{"Topic A", "Topic B", "Topic C"} {
					if !strings.Contains(string(agendaSlide), bullet) {
						t.Fatalf("expected agenda slide to contain %q, got: %q", bullet, string(agendaSlide))
					}
				}

				for _, slidePart := range []string{"ppt/slides/slide3.xml", "ppt/slides/slide4.xml", "ppt/slides/slide5.xml"} {
					if _, err := readPPTXEntry(filepath.Join(dir, "out.pptx"), slidePart); err != nil {
						t.Fatalf("expected generated content slide %q: %v", slidePart, err)
					}
				}
			},
		},
		{
			name: "read slides error",
			prepare: func(_ *testing.T, _ string) commands.PPTXCmd {
				return commands.PPTXCmd{Slides: "/no/slides.md", Template: "/no/template.pptx", Output: "/tmp/out.pptx"}
			},
			wantErr: true,
		},
		{
			name: "unparseable slides markdown",
			prepare: func(t *testing.T, dir string) commands.PPTXCmd {
				t.Helper()
				slidesPath := filepath.Join(dir, "bad_slides.md")
				if err := os.WriteFile(slidesPath, []byte("no meta markers here"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.PPTXCmd{Slides: slidesPath, Template: realPPTXTemplate, Output: filepath.Join(dir, "out.pptx")}
			},
			wantErr: true,
		},
		{
			name: "course_name var overrides config course name",
			prepare: func(t *testing.T, dir string) commands.PPTXCmd {
				t.Helper()
				slidesPath := writeSlidesMarkdownFile(t, dir)
				outPath := filepath.Join(dir, "out.pptx")
				return commands.PPTXCmd{
					Slides: slidesPath, Template: realPPTXTemplate, Output: outPath,
					Vars: map[string]string{"course_name": "Override University"},
				}
			},
			verify: func(t *testing.T, dir string) {
				t.Helper()
				titleSlide, err := readPPTXEntry(filepath.Join(dir, "out.pptx"), "ppt/slides/slide1.xml")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(titleSlide), "Override University") {
					t.Fatalf("expected title slide to contain course_name var override, got: %q", string(titleSlide))
				}
				if strings.Contains(string(titleSlide), "Test University") {
					t.Fatalf("expected config course name to be overridden, got: %q", string(titleSlide))
				}
			},
		},
		{
			name: "render pptx error, template not a valid pptx",
			prepare: func(t *testing.T, dir string) commands.PPTXCmd {
				t.Helper()
				slidesPath := writeSlidesMarkdownFile(t, dir)
				badTemplate := filepath.Join(dir, "bad_template.pptx")
				if err := os.WriteFile(badTemplate, []byte("not a zip"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commands.PPTXCmd{Slides: slidesPath, Template: badTemplate, Output: filepath.Join(dir, "out.pptx")}
			},
			wantErr: true,
		},
	}
}

func TestPPTXCmdRun_Table(t *testing.T) {
	t.Parallel()

	for _, tt := range pptxCmdTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfgPath := writePPTXConfigFile(t, dir, "Test University")
			cmd := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), &commands.CLI{Config: cfgPath})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.verify != nil {
				tt.verify(t, dir)
			}
		})
	}
}

func TestPPTXCmdRun_BadConfig(t *testing.T) {
	t.Parallel()
	err := (&commands.PPTXCmd{Slides: "/no/slides.md", Template: "/no/template.pptx", Output: "/tmp/out.pptx"}).
		Run(context.Background(), &commands.CLI{Config: "/no/such/quiz.json"})
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

// TestPPTXCmdRun_LogsRenderWarnings is a second test func against PPTXCmd.Run (alongside
// TestPPTXCmdRun_Table and TestPPTXCmdRun_BadConfig, already an established pattern in this
// file) because it needs its own PATH stub via t.Setenv, which — per this repo's Go test
// conventions — rules out t.Parallel() and so can't share the parallel table above.
func TestPPTXCmdRun_LogsRenderWarnings(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no pandoc on PATH at all

	dir := t.TempDir()
	cfgPath := writePPTXConfigFile(t, dir, "Test University")

	const md = `# Module 1

---

<!-- meta: 1 agenda -->
# Agenda

- Topic A
- Topic B
- Topic C

---

<!-- meta: 2 src01 -->
# Topic A

- See \(x^2 + y^2 = z^2\) for details
`
	slidesPath := filepath.Join(dir, "src01_slides.md")
	if err := os.WriteFile(slidesPath, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}

	var logBuf strings.Builder
	ctx := commands.WithLogger(t.Context(), audit.New(&logBuf))
	cmd := &commands.PPTXCmd{
		Slides:   slidesPath,
		Template: realPPTXTemplate,
		Output:   filepath.Join(dir, "out.pptx"),
	}
	if err := cmd.Run(ctx, &commands.CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := logBuf.String()
	if !strings.Contains(got, "pptx render warning") {
		t.Fatalf("expected a logged pptx render warning, got log output: %q", got)
	}
	if !strings.Contains(got, "x^2 + y^2 = z^2") {
		t.Fatalf("expected the logged warning to name the failed formula, got: %q", got)
	}
}

func TestExecute_PPTXCommandSuccess(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writePPTXConfigFile(t, dir, "Test University")
	slidesPath := writeSlidesMarkdownFile(t, dir)
	outPath := filepath.Join(dir, "out.pptx")

	withArgs(t, []string{
		"pdf2qti",
		"--config", cfgPath,
		"pptx",
		"--slides", slidesPath,
		"--output", outPath,
		realPPTXTemplate,
	})

	if err := commands.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
}

func readPPTXEntry(path, entry string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.Name != entry {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil {
			return nil, closeErr
		}
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, os.ErrNotExist
}
