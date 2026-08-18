package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

// ApproveCmd converts an approved quiz markdown draft to QTI.
type ApproveCmd struct{}

// Run executes the approve command.
func (a *ApproveCmd) Run(_ context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if err := runApproveSource(cfg, src, logger); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func runApproveSource(cfg *config.Config, src *config.Source, logger *audit.Logger) error {
	outDir := cfg.OutDir(src)
	quizFile := filepath.Join(outDir, src.ID+"_quiz.md")
	data, err := os.ReadFile(quizFile)
	if err != nil {
		return fmt.Errorf("read quiz file %q: %w", quizFile, err)
	}
	draft, err := render.ParseDraft(string(data))
	if err != nil {
		return fmt.Errorf("parse quiz draft: %w", err)
	}
	assessment, err := qti.BuildAssessment(draft)
	if err != nil {
		return fmt.Errorf("build assessment: %w", err)
	}
	xmlBytes, err := qti.Marshal(assessment)
	if err != nil {
		return fmt.Errorf("marshal QTI: %w", err)
	}
	packageFile := filepath.Join(outDir, src.ID+".zip")
	f, err := os.OpenFile(packageFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is constructed from trusted config values
	if err != nil {
		return fmt.Errorf("create QTI package: %w", err)
	}
	if err := qti.WritePackage(f, xmlBytes); err != nil {
		closeErr := f.Close()
		if closeErr != nil {
			return fmt.Errorf("write QTI package: %w (also close package: %v)", err, closeErr)
		}
		return fmt.Errorf("write QTI package: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close QTI package: %w", err)
	}
	logger.Info("wrote QTI package", "file", packageFile)
	return nil
}
