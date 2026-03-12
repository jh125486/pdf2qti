package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/extract"
)

// DistillCmd distills a PDF into a structured context JSON file.
type DistillCmd struct {
	Force bool     `name:"force" help:"Overwrite existing context file."`
	All   bool     `name:"all" help:"Distill all sources."`
	IDs   []string `arg:"" optional:"" help:"Source IDs to distill."`
}

// Run executes the distill command.
func (d *DistillCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)

	sources := d.selectSources(cfg)
	if len(sources) == 0 {
		return fmt.Errorf("no sources selected; specify source IDs or use --all")
	}

	llm := &stubDistillLLM{}
	for _, src := range sources {
		if err := runDistillSource(ctx, cfg, src, logger, llm, d.Force); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func (d *DistillCmd) selectSources(cfg *config.Config) []*config.Source {
	if d.All {
		ptrs := make([]*config.Source, len(cfg.Sources))
		for i := range cfg.Sources {
			ptrs[i] = &cfg.Sources[i]
		}
		return ptrs
	}
	if len(d.IDs) == 0 {
		return nil
	}
	idSet := make(map[string]bool, len(d.IDs))
	for _, id := range d.IDs {
		idSet[id] = true
	}
	var ptrs []*config.Source
	for i := range cfg.Sources {
		if idSet[cfg.Sources[i].ID] {
			ptrs = append(ptrs, &cfg.Sources[i])
		}
	}
	return ptrs
}

func runDistillSource(ctx context.Context, cfg *config.Config, src *config.Source, logger *audit.Logger, llm distill.LLM, force bool) error {
	outDir := cfg.OutDir(src)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create outDir %q: %w", outDir, err)
	}

	ctxFile := filepath.Join(outDir, src.ID+"_context.json")
	if !force {
		if _, err := os.Stat(ctxFile); err == nil {
			return fmt.Errorf("context file already exists: %q — use --force to overwrite", ctxFile)
		}
	}

	logger.Info("extracting PDF", "source", src.ID, "pdf", src.PDF)
	text, err := extract.ExtractText(src.PDF)
	if err != nil {
		return fmt.Errorf("extract PDF: %w", err)
	}

	logger.Info("distilling context", "source", src.ID)
	dc, err := distill.Distill(ctx, src, cfg.CourseObjectives, llm, text)
	if err != nil {
		return fmt.Errorf("distill: %w", err)
	}

	if err := distill.Save(ctxFile, dc); err != nil {
		return fmt.Errorf("save context: %w", err)
	}
	logger.Info("wrote context", "file", ctxFile)
	return nil
}

// stubDistillLLM is a placeholder LLM that returns a minimal context JSON.
// In a real implementation this would be replaced by an actual LLM client.
type stubDistillLLM struct{}

func (s *stubDistillLLM) Complete(_ context.Context, _ string) (string, error) {
	return `{
  "module_name": "",
  "text": "",
  "overview": "",
  "key_concepts": [],
  "material_overview": "",
  "teaching_notes": "",
  "objectives": []
}`, nil
}
