package commands

import (
	"context"
	"fmt"

	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/pptx"
)

// PPTXCmd renders a PPTX template against a distilled context JSON file.
type PPTXCmd struct {
	Context  string            `help:"Path to context JSON file."              required:""`
	Output   string            `help:"Output PPTX file path."                  required:""                        short:"o"`
	Vars     map[string]string `help:"Extra template vars as key=value pairs." mapsep:";"                         short:"v"`
	Template string            `arg:""                                         help:"Path to PPTX template file."`
}

// Run executes the pptx command.
func (p *PPTXCmd) Run(_ context.Context, _ *CLI) error {
	dc, err := distill.Load(p.Context)
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}
	if err := pptx.Render(p.Template, dc, p.Vars, p.Output); err != nil {
		return fmt.Errorf("render pptx: %w", err)
	}
	return nil
}
