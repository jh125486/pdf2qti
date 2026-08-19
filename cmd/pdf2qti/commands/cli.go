// Package commands contains the CLI command implementations.
package commands

import (
	"context"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jh125486/pdf2qti/internal/audit"
)

// CLI is the root command structure for pdf2qti.
type CLI struct {
	Config      string        `default:"quiz_input.json" help:"Path to config file."                                                                                       short:"c"`
	HTTPTimeout time.Duration `default:"5m"              help:"HTTP timeout for LLM API calls (e.g. \"10m\"). Content-heavy chapters can occasionally exceed the default."`
	Distill     DistillCmd    `cmd:""                    help:"Distill PDF into structured context JSON."`
	Generate    GenerateCmd   `cmd:""                    help:"Generate quiz draft from distilled context."`
	Approve     ApproveCmd    `cmd:""                    help:"Convert approved quiz markdown draft to QTI."`
	ImportBank  ImportBankCmd `cmd:""                    help:"Import QTI ZIP into Canvas New Quizzes Item Bank via browser UI."`
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
	runCtx := WithLogger(context.Background(), audit.New(os.Stdout))
	ctx := kong.Parse(&cli,
		kong.Name("pdf2qti"),
		kong.Description("Convert PDF sources to Canvas-compatible QTI quizzes."),
		kong.BindTo(runCtx, (*context.Context)(nil)),
		kong.UsageOnError(),
	)
	return ctx.Run(&cli)
}

// loggerCtxKey is the context key under which WithLogger stores an *audit.Logger.
type loggerCtxKey struct{}

// WithLogger returns a copy of ctx carrying l as its audit logger, retrievable by
// commands' Run methods via loggerFrom. Exported so callers embedding this package
// (and its own tests) can inject a logger without a package-level mutable var.
func WithLogger(ctx context.Context, l *audit.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

// loggerFrom returns the *audit.Logger carried by ctx, or a logger writing to os.Stderr
// if ctx has none (e.g. a Run called directly, outside Execute's kong.BindTo wiring).
func loggerFrom(ctx context.Context) *audit.Logger {
	if l, ok := ctx.Value(loggerCtxKey{}).(*audit.Logger); ok {
		return l
	}
	return audit.New(os.Stderr)
}
