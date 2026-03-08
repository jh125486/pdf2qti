package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Convert approved quiz markdown draft to QTI",
	RunE:  runApprove,
}

func runApprove(cmd *cobra.Command, _ []string) error {
	cfgPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("get config flag: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(os.Stdout)
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
	qtiFile := filepath.Join(outDir, src.ID+".qti")
	if err := os.WriteFile(qtiFile, xmlBytes, 0o644); err != nil {
		return fmt.Errorf("write QTI file: %w", err)
	}
	logger.Info("wrote QTI", "file", qtiFile)
	return nil
}
