// Package commands contains the CLI command implementations.
package commands

import (
	"context"
	"os"

	"github.com/alecthomas/kong"
)

// CLI is the root command structure for pdf2qti.
type CLI struct {
	Config   string      `short:"c" default:"quiz_input.json" help:"Path to config file."`
	Generate GenerateCmd `cmd:"" help:"Extract PDF and generate quiz draft."`
	Approve  ApproveCmd  `cmd:"" help:"Convert approved quiz markdown draft to QTI."`
	Validate ValidateCmd `cmd:"" help:"Validate quiz markdown draft."`
}

// Execute parses and runs the CLI.
func Execute() error {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("pdf2qti"),
		kong.Description("Convert PDF sources to Canvas-compatible QTI quizzes."),
		kong.UsageOnError(),
	)
	return ctx.Run(context.Background(), &cli)
}

// logOutput is the writer used for audit loggers; may be replaced in tests.
var logOutput = os.Stdout
