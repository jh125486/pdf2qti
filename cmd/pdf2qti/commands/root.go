// Package commands contains the CLI command implementations.
package commands

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pdf2qti",
	Short: "Convert PDF sources to Canvas-compatible QTI quizzes",
	Long:  `pdf2qti is a CLI tool that extracts content from PDFs and generates QTI quiz files for Canvas LMS.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("config", "c", "quiz_input.json", "path to config file")
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(validateCmd)
}
