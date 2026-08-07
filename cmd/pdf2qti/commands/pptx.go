package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/pptx"
)

// PPTXCmd renders a PPTX template against proto-deck slide Markdown, as produced by the
// `slides` command or hand-written in the same format.
type PPTXCmd struct {
	Slides   string            `help:"Path to slide deck Markdown file."       required:""`
	Output   string            `help:"Output PPTX file path."                  required:""                        short:"o"`
	Vars     map[string]string `help:"Extra template vars as key=value pairs." mapsep:";"                         short:"v"`
	Template string            `arg:""                                         help:"Path to PPTX template file." required:""`
}

// Run executes the pptx command.
func (p *PPTXCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	md, err := os.ReadFile(p.Slides)
	if err != nil {
		return fmt.Errorf("read slides %q: %w", p.Slides, err)
	}

	title, agenda, slides, err := distill.ParseProtoDeck(string(md))
	if err != nil {
		return fmt.Errorf("parse slides %q: %w", p.Slides, err)
	}

	courseName := cfg.CourseName
	if v, ok := p.Vars["course_name"]; ok {
		courseName = v
	}

	dc := &distill.DistilledContext{ModuleName: title, Agenda: agenda, Slides: slides}
	warnings, err := pptx.Render(p.Template, dc, courseName, p.Vars, p.Output)
	if err != nil {
		return fmt.Errorf("render pptx: %w", err)
	}
	logger := loggerFrom(ctx)
	for _, w := range warnings {
		logger.Warn("pptx render warning", "detail", w)
	}
	return nil
}
