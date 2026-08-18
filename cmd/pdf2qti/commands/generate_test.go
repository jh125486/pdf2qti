package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

type generateTestCase struct {
	name        string
	skipApprove bool
	prepare     func(t *testing.T, dir string) string
	wantErr     bool
	checkQTI    bool
}

// generateTestCases builds TestGenerateCmdRun_Table's table. Split out from the test function
// itself to keep gocyclo's complexity count on the (trivial) runner, not this literal.
func generateTestCases() []generateTestCase {
	return []generateTestCase{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir)
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
		},
		{
			name:        "skip approve builds QTI package",
			skipApprove: true,
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir)
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
			checkQTI: true,
		},
		{
			name: "description template and open review",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir)
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","descriptionTemplate":"Chapter {{.chapter}}","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `","openReview":true}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
		},
		{
			name: "bad description template continues with empty description",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir)
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","descriptionTemplate":"{{.chapter","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
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
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + dir + `/nonexistent.pdf"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
			wantErr: true,
		},
		{
			// No titleTemplate and an empty module_name/source-name both fall through: the
			// title-fallback loop walks all three candidates (dc.ModuleName, src.Name, src.ID),
			// landing on src.ID.
			name: "empty title template falls back through candidates to source id",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir) // module_name: ""
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
		},
		{
			// outDir's parent path component ("blocker") is pre-created as a regular file, so
			// os.MkdirAll(outDir, ...) fails with "not a directory".
			name: "create outDir error",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				blocker := filepath.Join(dir, "blocker")
				mustWriteFile(t, blocker, []byte("x"))
				outDir := filepath.Join(blocker, "nested")
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + outDir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
			wantErr: true,
		},
		{
			// Pre-creating a directory at the quiz output path makes os.WriteFile fail,
			// exercising the write-quiz-file error branch.
			name: "write quiz file error, output path is a directory",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				mustWriteFile(t, pdfPath, nil)
				writeContextFile(t, dir)
				if err := os.Mkdir(filepath.Join(dir, "src01_quiz.md"), 0o750); err != nil {
					t.Fatal(err)
				}
				cfgFile := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				mustWriteFile(t, cfgFile, []byte(cfgJSON))
				return cfgFile
			},
			wantErr: true,
		},
	}
}

func TestGenerateCmdRun_Table(t *testing.T) {
	t.Parallel()

	for _, tt := range generateTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfgFile := tt.prepare(t, dir)
			err := (&commands.GenerateCmd{SkipApprove: tt.skipApprove}).Run(context.Background(), &commands.CLI{Config: cfgFile})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.checkQTI {
				assertCanvasPackage(t, filepath.Join(dir, "src01.zip"))
			}
		})
	}
}
