package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/page"
)

// PageCmd renders a Go HTML template against a distilled context JSON file.
type PageCmd struct {
	Context  string            `short:"c" required:"" help:"Path to context JSON file."`
	Output   string            `short:"o" help:"Output file (default: stdout)."`
	Vars     map[string]string `short:"v" mapsep:";" help:"Extra template vars as key=value pairs."`
	Template string            `arg:"" help:"Path to Go HTML template."`
}

// Run executes the page command.
func (p *PageCmd) Run(_ context.Context, _ *CLI) error {
	dc, err := distill.Load(p.Context)
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}

	var out *os.File
	if p.Output != "" {
		f, err := os.Create(p.Output)
		if err != nil {
			return fmt.Errorf("create output file %q: %w", p.Output, err)
		}
		defer f.Close()
		out = f
	}

	if err := page.Render(p.Template, dc, p.Vars, out); err != nil {
		return fmt.Errorf("render page: %w", err)
	}
	return nil
}
