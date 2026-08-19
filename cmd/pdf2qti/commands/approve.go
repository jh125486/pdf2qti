package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

// ApproveCmd converts an approved quiz markdown draft to QTI.
type packageOps struct {
	open  func(string) (io.WriteCloser, error)
	write func(io.Writer, string, []byte) error
}

type ApproveCmd struct {
	packageOps packageOps
}

// Run executes the approve command.
func (a *ApproveCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := loggerFrom(ctx)
	ops := a.packageOps
	if ops.open == nil {
		ops.open = func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is constructed from trusted config values
		}
	}
	if ops.write == nil {
		ops.write = qti.WritePackage
	}
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if err := runApproveSourceWithOps(cfg, src, logger, ops); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func runApproveSource(cfg *config.Config, src *config.Source, logger *audit.Logger) error {
	return runApproveSourceWithOps(cfg, src, logger, packageOps{})
}

func runApproveSourceWithOps(cfg *config.Config, src *config.Source, logger *audit.Logger, ops packageOps) error {
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
	if ops.open == nil {
		ops.open = func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // path is constructed from trusted config values
		}
	}
	if ops.write == nil {
		ops.write = qti.WritePackage
	}
	f, err := ops.open(packageFile)
	if err != nil {
		return fmt.Errorf("create QTI package: %w", err)
	}
	if err := ops.write(f, src.ID+".xml", xmlBytes); err != nil {
		closeErr := f.Close()
		if closeErr != nil {
			return fmt.Errorf("write QTI package: %w (also close package: %w)", err, closeErr)
		}
		return fmt.Errorf("write QTI package: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close QTI package: %w", err)
	}
	logger.Info("wrote QTI package", "file", packageFile)
	return nil
}
