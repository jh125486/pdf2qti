// Package main is the entry point for the pdf2qti CLI.
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

//go:embed quiz_input.schema.json
var configSchema []byte

//go:embed slides.md
var slideMarkdown string

func main() {
	if err := commands.ExecuteWithDocumentation(commands.Documentation{ConfigSchema: configSchema, SlideMarkdown: slideMarkdown}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
