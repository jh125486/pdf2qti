package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

// ModuleCmd builds a combined Markdown document (and JSON) for a module spanning one or more
// already-distilled chapters.
type ModuleCmd struct {
	Force     bool   `help:"Overwrite existing module doc." name:"force"`
	MinSlides int    `default:"8"                           help:"Minimum total slides in the module deck."`
	MaxSlides int    `default:"30"                          help:"Maximum total slides in the module deck."`
	ID        string `arg:""                                help:"Module ID from config."`
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
	tags := make([]string, len(srcs))
	for i, src := range srcs {
		ctxFile := filepath.Join(cfg.OutDir(src), src.ID+"_context.json")
		dc, err := distill.Load(ctxFile)
		if err != nil {
			return fmt.Errorf("load context for source %q (run distill for it first): %w", src.ID, err)
		}
		chapters[i] = dc
		tags[i] = src.ID
	}

	llm := selectLLM(cfg.EffectiveGeneration(srcs[0]), logger, &stubModuleLLM{chapterTags: tags})
	logger.Info("building module doc", "module", mod.ID, "sources", len(chapters))
	doc, err := distill.BuildModuleDoc(ctx, llm, mod.ID, mod.Name, chapters, m.MinSlides, m.MaxSlides)
	if err != nil {
		return fmt.Errorf("build module doc: %w", err)
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
// ever answers one prompt shape), BuildModuleDoc issues two different prompts against the same
// LLM — a JSON-merge prompt and the proto-deck markdown prompt — so this stub distinguishes them
// by content. In a real implementation this would be replaced by an actual LLM client.
type stubModuleLLM struct {
	chapterTags []string
}

func (s *stubModuleLLM) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "prototype PowerPoint outline") {
		return stubProtoDeckMarkdown(prompt, s.chapterTags), nil
	}
	return `{"overview":"","objectives":[],"vocabulary":[],"theorems":[]}`, nil
}

var reTargetSlideRange = regexp.MustCompile(`TARGET_SLIDE_RANGE: min=(\d+) max=(\d+)`)

// stubProtoDeckMarkdown builds a minimal but valid proto-deck markdown (sequential meta markers,
// slide count within the range requested in prompt) for stubProtoDeckMarkdown's caller.
func stubProtoDeckMarkdown(prompt string, chapterTags []string) string {
	n := 8
	if m := reTargetSlideRange.FindStringSubmatch(prompt); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
			n = v
		}
	}
	if n < 2 {
		n = 2
	}

	var b strings.Builder
	b.WriteString("# Module Deck\n\n---\n\n<!-- meta: 1 agenda -->\n# Agenda\n\n- placeholder\n\n---\n\n")
	for i := 2; i < n; i++ {
		tag := chapterTags[(i-2)%len(chapterTags)]
		fmt.Fprintf(&b, "<!-- meta: %d %s -->\n# Slide %d\n\n- placeholder\n\n---\n\n", i, tag, i)
	}
	fmt.Fprintf(&b, "<!-- meta: %d summary -->\n# Summary\n\n- placeholder\n", n)
	return b.String()
}
