// Package commands contains the CLI command implementations.
package commands

import (
	"context"
	"io"
	"os"

	"github.com/alecthomas/kong"
)

// CLI is the root command structure for pdf2qti.
type CLI struct {
	Config   string      `default:"quiz_input.json" help:"Path to config file."                                     short:"c"`
	Distill  DistillCmd  `cmd:""                    help:"Distill PDF into structured context JSON."`
	Generate GenerateCmd `cmd:""                    help:"Generate quiz draft from distilled context."`
	Approve  ApproveCmd  `cmd:""                    help:"Convert approved quiz markdown draft to QTI."`
	Validate ValidateCmd `cmd:""                    help:"Validate quiz markdown draft."`
	Page     PageCmd     `cmd:""                    help:"Render HTML page from distilled context and template."`
	PPTX     PPTXCmd     `cmd:""                    help:"Render PPTX from distilled context and template."`
	Publish  PublishCmd  `cmd:""                    help:"Render and publish Canvas pages for each module context."`
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
