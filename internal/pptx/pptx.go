// Package pptx provides PPTX rendering from distilled context and text templates.
package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jh125486/pdf2qti/internal/distill"
)

// Render reads a PPTX template file, executes Go text templates in XML and RELS
// parts against distilled context data, and writes a new PPTX to outputPath.
func Render(templatePath string, dc *distill.DistilledContext, vars map[string]string, outputPath string) error {
	inData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read pptx template %q: %w", templatePath, err)
	}

	reader, err := zip.NewReader(bytes.NewReader(inData), int64(len(inData)))
	if err != nil {
		return fmt.Errorf("open pptx template %q: %w", templatePath, err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output %q: %w", outputPath, err)
	}
	defer outFile.Close()

	writer := zip.NewWriter(outFile)
	defer writer.Close()

	data := buildData(dc, vars)

	for _, file := range reader.File {
		if err := copyEntry(writer, file, data); err != nil {
			return err
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize output pptx: %w", err)
	}

	return nil
}

func copyEntry(writer *zip.Writer, file *zip.File, data map[string]any) error {
	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("open template entry %q: %w", file.Name, err)
	}
	defer rc.Close()

	entryWriter, err := writer.CreateHeader(&file.FileHeader)
	if err != nil {
		return fmt.Errorf("create output entry %q: %w", file.Name, err)
	}

	if !isTemplatedPart(file.Name) {
		if file.UncompressedSize64 > math.MaxInt64 {
			return fmt.Errorf("copy entry %q: entry too large", file.Name)
		}
		expectedSize := int64(file.UncompressedSize64)
		n, err := io.CopyN(entryWriter, rc, expectedSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("copy entry %q: %w", file.Name, err)
		}
		if n != expectedSize {
			return fmt.Errorf("copy entry %q: expected %d bytes, got %d", file.Name, file.UncompressedSize64, n)
		}
		return nil
	}

	tplBytes, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read template entry %q: %w", file.Name, err)
	}
	tmpl, err := template.New(file.Name).Parse(string(tplBytes))
	if err != nil {
		return fmt.Errorf("parse template entry %q: %w", file.Name, err)
	}
	if err := tmpl.Execute(entryWriter, data); err != nil {
		return fmt.Errorf("execute template entry %q: %w", file.Name, err)
	}
	return nil
}

func isTemplatedPart(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels")
}

func buildData(dc *distill.DistilledContext, vars map[string]string) map[string]any {
	data := map[string]any{
		"source_id":         dc.SourceID,
		"book":              dc.Book,
		"chapter":           dc.Chapter,
		"module_name":       dc.ModuleName,
		"overview":          dc.Overview,
		"key_concepts":      dc.KeyConcepts,
		"material_overview": dc.MaterialOverview,
		"teaching_notes":    dc.TeachingNotes,
		"objectives":        dc.Objectives,
	}
	for k, v := range vars {
		data[k] = v
	}
	return data
}
