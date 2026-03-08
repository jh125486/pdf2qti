// Package main is the entry point for the pdf2qti CLI.
package main

import (
	"fmt"
	"os"

	"github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
