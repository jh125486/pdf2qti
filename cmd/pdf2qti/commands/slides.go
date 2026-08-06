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

// SlidesCmd generates proto-deck slide Markdown from one or more already-distilled chapters.
// It's the first half of a two-command pipeline: `slides` (context -> Markdown) feeds `pptx`
// (Markdown + template -> .pptx) as a separate step, so a deck can also be hand-written or
// hand-edited between the two without needing a distilled context at all.
type SlidesCmd struct {
	Force     bool     `help:"Overwrite existing output file."                                             name:"force"`
	All       bool     `help:"Generate slides for all sources."                                            name:"all"`
	MinSlides int      `help:"Minimum total slides in the deck. Unset or 0: auto-scale to chapter length."`
	MaxSlides int      `help:"Maximum total slides in the deck. Unset or 0: auto-scale to chapter length."`
	Output    string   `help:"Output Markdown file path."                                                  short:"o"`
	IDs       []string `arg:""                                                                             help:"Source IDs to build a deck from." optional:""`
}

// Run executes the slides command.
func (s *SlidesCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)

	srcs := s.selectSources(cfg)
	if len(srcs) == 0 {
		return fmt.Errorf("no sources selected; specify source IDs or use --all")
	}

	outFile := s.Output
	if outFile == "" {
		outFile = filepath.Join(cfg.OutDir(srcs[0]), srcs[0].ID+"_slides.md")
	}
	if !s.Force {
		if _, err := os.Stat(outFile); err == nil {
			return fmt.Errorf("output file already exists: %q — use --force to overwrite", outFile)
		}
	}

	chapters := make([]distill.ProtoChapterInput, len(srcs))
	for i, src := range srcs {
		ctxFile := filepath.Join(cfg.OutDir(src), src.ID+"_context.json")
		dc, err := distill.Load(ctxFile)
		if err != nil {
			return fmt.Errorf("load context for source %q (run distill for it first): %w", src.ID, err)
		}
		chapters[i] = distill.ProtoChapterInput{
			Tag:           src.ID,
			ModuleName:    dc.ModuleName,
			Overview:      dc.Overview,
			KeyConcepts:   dc.KeyConcepts,
			TeachingNotes: dc.TeachingNotes,
			Text:          dc.Text,
			Sections:      []distill.Section(dc.Sections),
		}
	}

	minSlides, maxSlides := resolveSlideRange(s.MinSlides, s.MaxSlides, chapters)

	llm := selectLLM(cfg.EffectiveGeneration(srcs[0]), cli.HTTPTimeout, logger, &stubSlidesLLM{})
	logger.Info("generating slide deck", "sources", len(chapters), "minSlides", minSlides, "maxSlides", maxSlides)
	deck, warnings, err := distill.GenerateProtoDeck(ctx, llm, chapters, minSlides, maxSlides)
	if err != nil {
		return fmt.Errorf("generate proto deck: %w", err)
	}
	for _, w := range warnings {
		logger.Warn("proto deck warning", "detail", w)
	}

	if err := os.WriteFile(outFile, []byte(deck), 0o600); err != nil {
		return fmt.Errorf("write slides %q: %w", outFile, err)
	}
	logger.Info("wrote slides", "file", outFile)
	return nil
}

// resolveSlideRange returns (minSlides, maxSlides) as given whenever both are already positive
// (an explicit user override), and otherwise falls back to distill.AutoSlideRange(chapters) for
// whichever of the two is unset (<=0) — so `--min-slides 40` alone still auto-scales the max.
//
// An explicit bound always wins over its auto-derived counterpart: if the user pins one side and
// the auto-derived other side would make the range invalid (min > max) — e.g. `--min-slides 60`
// against an auto max of 45 — the auto side is pulled to match the explicit one instead of
// returning an invalid range for GenerateProtoDeck to reject.
func resolveSlideRange(minSlides, maxSlides int, chapters []distill.ProtoChapterInput) (resolvedMin, resolvedMax int) {
	if minSlides > 0 && maxSlides > 0 {
		return minSlides, maxSlides
	}
	autoMin, autoMax := distill.AutoSlideRange(chapters)
	resolvedMin, resolvedMax = minSlides, maxSlides
	if resolvedMin <= 0 {
		resolvedMin = autoMin
	}
	if resolvedMax <= 0 {
		resolvedMax = autoMax
	}
	if resolvedMin > resolvedMax {
		if minSlides > 0 {
			resolvedMax = resolvedMin
		} else {
			resolvedMin = resolvedMax
		}
	}
	return resolvedMin, resolvedMax
}

func (s *SlidesCmd) selectSources(cfg *config.Config) []*config.Source {
	if s.All {
		ptrs := make([]*config.Source, len(cfg.Sources))
		for i := range cfg.Sources {
			ptrs[i] = &cfg.Sources[i]
		}
		return ptrs
	}
	if len(s.IDs) == 0 {
		return nil
	}
	idSet := make(map[string]bool, len(s.IDs))
	for _, id := range s.IDs {
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

// stubSlidesLLM is a placeholder LLM for the slides command, used only when no real provider key
// is configured (see selectLLM). SlidesCmd only ever issues GenerateProtoDeck's prompt shapes
// (see stubProtoDeckShape in llm.go), unlike ModuleCmd, which also issues a JSON-merge prompt.
type stubSlidesLLM struct{}

func (s *stubSlidesLLM) Complete(_ context.Context, prompt string, _ *distill.Schema) (string, error) {
	resp, _ := stubProtoDeckShape(prompt)
	return resp, nil
}
