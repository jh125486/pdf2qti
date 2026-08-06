// Package commands contains the CLI command implementations.
package commands

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

// CLI is the root command structure for pdf2qti.
type CLI struct {
	Config      string        `default:"quiz_input.json" help:"Path to config file."                                                                                       short:"c"`
	HTTPTimeout time.Duration `default:"5m"              help:"HTTP timeout for LLM API calls (e.g. \"10m\"). Content-heavy chapters can occasionally exceed the default."`
	Distill     DistillCmd    `cmd:""                    help:"Distill PDF into structured context JSON."`
	Generate    GenerateCmd   `cmd:""                    help:"Generate quiz draft from distilled context."`
	Approve     ApproveCmd    `cmd:""                    help:"Convert approved quiz markdown draft to QTI."`
	Validate    ValidateCmd   `cmd:""                    help:"Validate quiz markdown draft."`
	Page        PageCmd       `cmd:""                    help:"Render HTML page from distilled context and template."`
	Slides      SlidesCmd     `cmd:""                    help:"Generate proto-deck slide Markdown from distilled context."`
	PPTX        PPTXCmd       `cmd:""                    help:"Render PPTX from slide Markdown and template."`
	Publish     PublishCmd    `cmd:""                    help:"Render and publish Canvas pages for each module context."`
	Module      ModuleCmd     `cmd:""                    help:"Build a combined Markdown doc for a module spanning several chapters."`
}

// Execute parses and runs the CLI.
func Execute() error {
	var cli CLI
	runCtx := context.Background()
	ctx := kong.Parse(&cli,
		kong.Name("pdf2qti"),
		kong.Description("Convert PDF sources to Canvas-compatible QTI quizzes."),
		kong.BindTo(runCtx, (*context.Context)(nil)),
		kong.UsageOnError(),
	)
	return ctx.Run(&cli)
}

// logOutput is the writer used for audit loggers; may be replaced in tests.
var logOutput io.Writer = os.Stdout
