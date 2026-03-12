package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
)

// validQuizMD is a minimal quiz draft with a title and one valid MC question.
const validQuizMD = `# Test Quiz

## MC

1. What is 2+2?
   [ ] 3
   [*] 4
   [ ] 5
`

// invalidQuizMD has a question with no correct answer → BuildAssessment error.
const invalidQuizMD = `# Test Quiz

## MC

1. What is 2+2?
   [ ] 3
   [ ] 4
`

// validationFailQuizMD has sequential numbering errors.
const validationFailQuizMD = `# Test Quiz

## MC

2. What is 2+2?
   [*] 4
`

func testConfig(t *testing.T, outDir, pdfPath string) *config.Config {
	t.Helper()
	return &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{OutDir: outDir},
			Quiz: config.Quiz{
				TitleTemplate: "Test Quiz",
				Counts:        config.Counts{TF: 1, MC: 1},
			},
			Validation: config.Validation{
				RequireSequentialNumbering: true,
			},
		},
		Sources: []config.Source{
			{ID: "src01", PDF: pdfPath},
		},
	}
}

func silentLogger() *audit.Logger {
	return audit.New(io.Discard)
}

// ── runApproveSource ──────────────────────────────────────────────────────────

func TestRunApproveSource_Success(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	src := &cfg.Sources[0]

	if err := runApproveSource(cfg, src, silentLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qtiFile := filepath.Join(dir, "src01.qti")
	if _, err := os.Stat(qtiFile); err != nil {
		t.Errorf("expected QTI file to be created: %v", err)
	}
}

func TestRunApproveSource_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	err := runApproveSource(cfg, src, silentLogger())
	if err == nil {
		t.Fatal("expected error for missing quiz file")
	}
}

func TestRunApproveSource_BuildError(t *testing.T) {
	dir := t.TempDir()
	// Quiz file with no title → qti.BuildAssessment returns error
	noTitleMD := "## MC\n\n1. What?\n   [*] A\n"
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(noTitleMD), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	err := runApproveSource(cfg, src, silentLogger())
	if err == nil {
		t.Fatal("expected error when quiz has no title")
	}
}

// ── runValidateSource ─────────────────────────────────────────────────────────

func TestRunValidateSource_Success(t *testing.T) {
	dir := t.TempDir()
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	valid, err := runValidateSource(cfg, src, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected validation to pass")
	}
}

func TestRunValidateSource_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	_, err := runValidateSource(cfg, src, silentLogger())
	if err == nil {
		t.Fatal("expected error for missing quiz file")
	}
}

func TestRunValidateSource_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validationFailQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	valid, err := runValidateSource(cfg, src, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected validation to fail for mis-numbered question")
	}
}

// ── runGenerateSource ─────────────────────────────────────────────────────────

func TestRunGenerateSource_Success(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	src := &cfg.Sources[0]

	// swap logOutput for the duration of the test
	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	quizFile := filepath.Join(dir, "src01_quiz.md")
	if _, err := os.Stat(quizFile); err != nil {
		t.Errorf("expected quiz draft file to be created: %v", err)
	}
	ctxFile := filepath.Join(dir, "src01_context.md")
	if _, err := os.Stat(ctxFile); err != nil {
		t.Errorf("expected context file to be created: %v", err)
	}
}

func TestRunGenerateSource_SkipApprove(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	// skipApprove=true → generates draft then immediately converts to QTI
	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qtiFile := filepath.Join(dir, "src01.qti")
	if _, err := os.Stat(qtiFile); err != nil {
		t.Errorf("expected QTI file from skip-approve path: %v", err)
	}
}

func TestRunGenerateSource_OpenReview(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	// Enable open-review so that branch is exercised
	cfg.Defaults.Workflow.OpenReview = true
	cfg.Defaults.Workflow.ReviewTarget = "some-editor"
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateSource_MissingPDF(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir, filepath.Join(dir, "nonexistent.pdf"))
	src := &cfg.Sources[0]

	err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false)
	if err == nil {
		t.Fatal("expected error for missing PDF file")
	}
}

func TestRunGenerateSource_DescriptionTemplate(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	cfg.Defaults.Quiz.DescriptionTemplate = "Chapter {{.chapter}} description"
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateSource_BadDescriptionTemplate(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	// Bad template will error at execute; generate should log and continue
	cfg.Defaults.Quiz.DescriptionTemplate = "{{call .}}" // execute error
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	// Should not fail overall even when description template errors
	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateSource_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a subdirectory as outDir; make dir read-only so MkdirAll fails
	outDir := filepath.Join(dir, "output")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod directory")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	cfg := testConfig(t, outDir, pdfPath)
	src := &cfg.Sources[0]

	err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false)
	if err == nil {
		t.Fatal("expected error when outDir cannot be created")
	}
}

func TestRunGenerateSource_WriteCtxError(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// outDir already exists; make it read-only so WriteFile fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod directory")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	cfg := testConfig(t, dir, pdfPath)
	src := &cfg.Sources[0]

	err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false)
	if err == nil {
		t.Fatal("expected error writing context file to read-only dir")
	}
}

func TestRunGenerateSource_WriteQuizError(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a directory at the quiz file path so WriteFile fails with "is a directory"
	quizPath := filepath.Join(dir, "src01_quiz.md")
	if err := os.MkdirAll(quizPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false)
	if err == nil {
		t.Fatal("expected error writing quiz draft when path is a directory")
	}
}



func TestValidateCmd_Run_ValidationFails(t *testing.T) {
	dir := t.TempDir()
	// Write a config file
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"},"validation":{"requireSequentialNumbering":true}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write a quiz file with validation errors
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validationFailQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ValidateCmd{}
	cli := &CLI{Config: cfgFile}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error from validation failure")
	}
}

func TestValidateCmd_Run_LoadConfigError(t *testing.T) {
	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ValidateCmd{}
	cli := &CLI{Config: "nonexistent_config.json"}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestValidateCmd_Run_SourceError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// No quiz file → runValidateSource returns error

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ValidateCmd{}
	cli := &CLI{Config: cfgFile}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error when quiz file is missing")
	}
}

func TestValidateCmd_Run_Success(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ValidateCmd{}
	cli := &CLI{Config: cfgFile}
	if err := cmd.Run(context.Background(), cli); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── ApproveCmd.Run ────────────────────────────────────────────────────────────

func TestApproveCmd_Run_Success(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ApproveCmd{}
	cli := &CLI{Config: cfgFile}
	if err := cmd.Run(context.Background(), cli); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qtiFile := filepath.Join(dir, "src01.qti")
	if _, err := os.Stat(qtiFile); err != nil {
		t.Errorf("expected QTI file to exist: %v", err)
	}
}

func TestApproveCmd_Run_LoadConfigError(t *testing.T) {
	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ApproveCmd{}
	cli := &CLI{Config: "nonexistent_config.json"}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestApproveCmd_Run_SourceError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// No quiz file → runApproveSource fails

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &ApproveCmd{}
	cli := &CLI{Config: cfgFile}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error when quiz file is missing")
	}
}

// ── GenerateCmd.Run ───────────────────────────────────────────────────────────

func TestGenerateCmd_Run_Success(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"quiz":{"titleTemplate":"Test Quiz","counts":{"tf":1,"mc":1}},"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &GenerateCmd{}
	cli := &CLI{Config: cfgFile}
	if err := cmd.Run(context.Background(), cli); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	quizFile := filepath.Join(dir, "src01_quiz.md")
	if _, err := os.Stat(quizFile); err != nil {
		t.Errorf("expected quiz draft to exist: %v", err)
	}
}

func TestGenerateCmd_Run_LoadConfigError(t *testing.T) {
	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &GenerateCmd{}
	cli := &CLI{Config: "nonexistent_config.json"}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestGenerateCmd_Run_SourceError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	// PDF path that doesn't exist
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + dir + `/nonexistent.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &GenerateCmd{}
	cli := &CLI{Config: cfgFile}
	err := cmd.Run(context.Background(), cli)
	if err == nil {
		t.Fatal("expected error for missing PDF")
	}
}

// ── runGenerateSource title fallbacks ─────────────────────────────────────────

func TestRunGenerateSource_TitleFallbackToName(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	// Empty title template → falls back to src.Name
	cfg.Defaults.Quiz.TitleTemplate = ""
	cfg.Sources[0].Name = "My Source"
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateSource_TitleFallbackToID(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, dir, pdfPath)
	// Empty title template AND empty Name → falls back to src.ID
	cfg.Defaults.Quiz.TitleTemplate = ""
	cfg.Sources[0].Name = ""
	src := &cfg.Sources[0]

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runGenerateSource(context.Background(), cfg, src, silentLogger(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── runApproveSource write error ─────────────────────────────────────────────

func TestRunApproveSource_WriteError(t *testing.T) {
	dir := t.TempDir()
	quizFile := filepath.Join(dir, "src01_quiz.md")
	if err := os.WriteFile(quizFile, []byte(validQuizMD), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the directory read-only so WriteFile for QTI fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot chmod directory")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	cfg := testConfig(t, dir, "src01.pdf")
	src := &cfg.Sources[0]

	err := runApproveSource(cfg, src, silentLogger())
	if err == nil {
		t.Fatal("expected error writing QTI to read-only directory")
	}
}

