package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/generate"
	"github.com/jh125486/pdf2qti/internal/render"
)

// GenerateCmd extracts a PDF and generates a quiz draft.
type GenerateCmd struct {
	SkipApprove bool `name:"skip-approve" help:"Skip human review and run approve immediately."`
}

// Run executes the generate command.
func (g *GenerateCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if err := runGenerateSource(ctx, cfg, src, logger, g.SkipApprove); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func runGenerateSource(ctx context.Context, cfg *config.Config, src *config.Source, logger *audit.Logger, skipApprove bool) error { //nolint:gocyclo // complex but cohesive orchestration function
	wf := cfg.EffectiveWorkflow(src)
	outDir := cfg.OutDir(src)
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("create outDir %q: %w", outDir, err)
	}

	ctxFile := filepath.Join(outDir, src.ID+"_context.json")
	dc, err := distill.Load(ctxFile)
	if err != nil {
		return fmt.Errorf("context not found for %q — run `pdf2qti distill %s` first: %w", src.ID, src.ID, err)
	}
	logger.Info("loaded context", "file", ctxFile)

	text := dc.Text

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

	titleData := map[string]any{"name": src.Name, "chapter": src.Chapter, "module_name": dc.ModuleName}
	title, err := render.ExecuteTemplate(q.TitleTemplate, titleData)
	if err != nil || title == "" {
		for _, candidate := range []string{dc.ModuleName, src.Name, src.ID} {
			if candidate != "" {
				title = candidate
				break
			}
		}
	}
	desc := ""
	if q.DescriptionTemplate != "" {
		if d, err := render.ExecuteTemplate(q.DescriptionTemplate, titleData); err != nil {
			logger.Info("failed to execute description template; using empty description", "error", err)
		} else {
			desc = d
		}
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
	if err := os.WriteFile(quizFile, []byte(md), 0o600); err != nil {
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
