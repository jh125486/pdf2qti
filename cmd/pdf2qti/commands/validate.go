package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/render"
	"github.com/jh125486/pdf2qti/internal/validate"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate quiz markdown draft",
	RunE:  runValidate,
}

func runValidate(cmd *cobra.Command, _ []string) error {
	cfgPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("get config flag: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(os.Stdout)
	allValid := true
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		valid, err := runValidateSource(cfg, src, logger)
		if err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
		if !valid {
			allValid = false
		}
	}
	if !allValid {
		return fmt.Errorf("validation failed")
	}
	return nil
}

func runValidateSource(cfg *config.Config, src *config.Source, logger *audit.Logger) (bool, error) {
	outDir := cfg.OutDir(src)
	quizFile := filepath.Join(outDir, src.ID+"_quiz.md")
	data, err := os.ReadFile(quizFile)
	if err != nil {
		return false, fmt.Errorf("read quiz file %q: %w", quizFile, err)
	}
	draft, err := render.ParseDraft(string(data))
	if err != nil {
		return false, fmt.Errorf("parse quiz draft: %w", err)
	}
	v := cfg.EffectiveValidation(src)
	result := validate.ValidateDraft(draft, v)
	for _, e := range result.Errors {
		logger.Error("validation error", "source", src.ID, "error", e)
	}
	for _, w := range result.Warnings {
		logger.Warn("validation warning", "source", src.ID, "warning", w)
	}
	if result.IsValid() {
		logger.Info("validation passed", "source", src.ID)
	}
	return result.IsValid(), nil
}
