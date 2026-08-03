package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

// ModuleCmd builds a combined Markdown document (and JSON) for a module spanning one or more
// already-distilled chapters.
type ModuleCmd struct {
	Force     bool   `help:"Overwrite existing module doc."                                                     name:"force"`
	MinSlides int    `help:"Minimum total slides in the module deck. Unset or 0: auto-scale to chapter length."`
	MaxSlides int    `help:"Maximum total slides in the module deck. Unset or 0: auto-scale to chapter length."`
	ID        string `arg:""                                                                                    help:"Module ID from config."`
}

// Run executes the module command.
func (m *ModuleCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)

	mod, err := cfg.ModuleByID(m.ID)
	if err != nil {
		return err
	}
	srcs, err := cfg.SourcesForModule(mod)
	if err != nil {
		return err
	}

	outDir := cfg.OutDir(srcs[0])
	docFile := filepath.Join(outDir, mod.ID+"_module.json")
	mdFile := filepath.Join(outDir, mod.ID+"_module.md")
	if !m.Force {
		if _, err := os.Stat(mdFile); err == nil {
			return fmt.Errorf("module doc already exists: %q — use --force to overwrite", mdFile)
		}
	}

	chapters := make([]*distill.DistilledContext, len(srcs))
	for i, src := range srcs {
		ctxFile := filepath.Join(cfg.OutDir(src), src.ID+"_context.json")
		dc, err := distill.Load(ctxFile)
		if err != nil {
			return fmt.Errorf("load context for source %q (run distill for it first): %w", src.ID, err)
		}
		chapters[i] = dc
	}

	protoChapters := make([]distill.ProtoChapterInput, len(chapters))
	for i, dc := range chapters {
		protoChapters[i] = distill.ProtoChapterInput{Text: dc.Text}
	}
	minSlides, maxSlides := resolveSlideRange(m.MinSlides, m.MaxSlides, protoChapters)

	llm := selectLLM(cfg.EffectiveGeneration(srcs[0]), logger, &stubModuleLLM{})
	logger.Info("building module doc", "module", mod.ID, "sources", len(chapters), "minSlides", minSlides, "maxSlides", maxSlides)
	doc, err := distill.BuildModuleDoc(ctx, llm, mod.ID, mod.Name, chapters, minSlides, maxSlides)
	if err != nil {
		return fmt.Errorf("build module doc: %w", err)
	}
	if n := len(doc.SlideWarnings); n > 0 {
		logger.Warn("proto deck warnings", "count", n)
	}

	if err := distill.SaveModuleDoc(docFile, doc); err != nil {
		return fmt.Errorf("save module doc: %w", err)
	}
	if err := distill.SaveModuleMarkdown(mdFile, distill.RenderModuleMarkdown(doc)); err != nil {
		return fmt.Errorf("save module markdown: %w", err)
	}
	logger.Info("wrote module doc", "json", docFile, "markdown", mdFile)
	return nil
}

// stubModuleLLM is a placeholder LLM for the module command. Unlike stubDistillLLM (which only
// ever answers one prompt shape), BuildModuleDoc issues several different prompts against the
// same LLM — a JSON-merge prompt plus GenerateProtoDeck's prompt shapes (see stubProtoDeckShape
// in llm.go) — so this stub distinguishes them by content.
type stubModuleLLM struct{}

func (s *stubModuleLLM) Complete(_ context.Context, prompt string, _ *distill.Schema) (string, error) {
	if resp, ok := stubProtoDeckShape(prompt); ok {
		return resp, nil
	}
	return `{"overview":"","objectives":[],"vocabulary":[],"theorems":[]}`, nil
}
