package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/extract"
	"github.com/jh125486/pdf2qti/internal/generate"
	"github.com/jh125486/pdf2qti/internal/render"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Extract PDF and generate quiz draft",
	RunE:  runGenerate,
}

func init() {
	generateCmd.Flags().Bool("skip-approve", false, "skip human review and run approve immediately")
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()
	cfgPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("get config flag: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(os.Stdout)
	skipApprove, err := cmd.Flags().GetBool("skip-approve")
	if err != nil {
		return fmt.Errorf("get skip-approve flag: %w", err)
	}

	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if err := runGenerateSource(ctx, cfg, src, logger, skipApprove); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func runGenerateSource(ctx context.Context, cfg *config.Config, src *config.Source, logger *audit.Logger, skipApprove bool) error {
	wf := cfg.EffectiveWorkflow(src)
	outDir := cfg.OutDir(src)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create outDir %q: %w", outDir, err)
	}

	logger.Info("extracting PDF", "source", src.ID, "pdf", src.PDF)
	text, err := extract.ExtractText(src.PDF)
	if err != nil {
		return fmt.Errorf("extract PDF: %w", err)
	}

	ctxFile := filepath.Join(outDir, src.ID+"_context.md")
	if err := os.WriteFile(ctxFile, []byte("# "+src.Name+" Context\n\n"+text+"\n"), 0o644); err != nil {
		return fmt.Errorf("write context file: %w", err)
	}
	logger.Info("wrote context", "file", ctxFile)

	gen := generate.New(cfg.EffectiveGeneration(src))
	q := cfg.EffectiveQuiz(src)

	tfQs, err := gen.GenerateStage(ctx, config.StageTF, text, q.Counts.TF)
	if err != nil {
		return fmt.Errorf("generate TF: %w", err)
	}
	maQs, err := gen.GenerateStage(ctx, config.StageMA, text, q.Counts.MA)
	if err != nil {
		return fmt.Errorf("generate MA: %w", err)
	}
	mcQs, err := gen.GenerateStage(ctx, config.StageMC, text, q.Counts.MC)
	if err != nil {
		return fmt.Errorf("generate MC: %w", err)
	}

	// Number sequentially
	offset := 0
	for i := range tfQs {
		tfQs[i].Number = offset + i + 1
	}
	offset += len(tfQs)
	for i := range maQs {
		maQs[i].Number = offset + i + 1
	}
	offset += len(maQs)
	for i := range mcQs {
		mcQs[i].Number = offset + i + 1
	}

	titleData := map[string]any{"name": src.Name, "chapter": src.Chapter}
	title, err := render.ExecuteTemplate(q.TitleTemplate, titleData)
	if err != nil {
		title = src.Name
	}
	desc := ""
	if q.DescriptionTemplate != "" {
		desc, _ = render.ExecuteTemplate(q.DescriptionTemplate, titleData)
	}

	draft := &render.QuizDraft{
		Title:       title,
		Description: desc,
		TFQuestions: tfQs,
		MAQuestions: maQs,
		MCQuestions: mcQs,
	}
	md, err := render.RenderDraft(draft)
	if err != nil {
		return fmt.Errorf("render draft: %w", err)
	}

	quizFile := filepath.Join(outDir, src.ID+"_quiz.md")
	if err := os.WriteFile(quizFile, []byte(md), 0o644); err != nil {
		return fmt.Errorf("write quiz file: %w", err)
	}
	logger.Info("wrote quiz draft", "file", quizFile)

	if !skipApprove && wf.OpenReview {
		logger.Info("open review (skipped in non-interactive mode)", "target", wf.ReviewTarget)
	}

	if skipApprove {
		return runApproveSource(cfg, src, logger)
	}
	return nil
}
