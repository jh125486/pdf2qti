package commands_test

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestPPTXCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) commands.PPTXCmd
		wantErr bool
		verify  func(t *testing.T, dir string)
	}{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) commands.PPTXCmd {
				t.Helper()
				contextPath := filepath.Join(dir, "src01_context.json")
				writeDistilledContextFile(t, dir)
				templatePath := filepath.Join(dir, "template.pptx")
				outPath := filepath.Join(dir, "out.pptx")
				if err := writePPTXTemplate(templatePath, `<a:t>{{.module_name}}</a:t>`); err != nil {
					t.Fatal(err)
				}
				return commands.PPTXCmd{Context: contextPath, Template: templatePath, Output: outPath}
			},
			verify: func(t *testing.T, dir string) {
				t.Helper()
				slideText, err := readPPTXEntry(filepath.Join(dir, "out.pptx"), "ppt/slides/slide1.xml")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(slideText), "Module 1") {
					t.Fatalf("expected rendered module name, got: %q", string(slideText))
				}
			},
		},
		{
			name: "load context error",
			prepare: func(_ *testing.T, _ string) commands.PPTXCmd {
				return commands.PPTXCmd{Context: "/no/context.json", Template: "/no/template.pptx", Output: "/tmp/out.pptx"}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), &commands.CLI{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.verify != nil {
				tt.verify(t, dir)
			}
		})
	}
}

func TestExecute_PPTXCommandSuccess(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir)
	templatePath := filepath.Join(dir, "template.pptx")
	outPath := filepath.Join(dir, "out.pptx")
	if err := writePPTXTemplate(templatePath, `<a:t>{{.module_name}}</a:t>`); err != nil {
		t.Fatal(err)
	}

	withArgs(t, []string{
		"pdf2qti",
		"--config", filepath.Join(dir, "unused.json"),
		"pptx",
		"--context", filepath.Join(dir, "src01_context.json"),
		"--output", outPath,
		templatePath,
	})

	if err := commands.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
}

func writePPTXTemplate(path, slideXML string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	entries := map[string]string{
		"[Content_Types].xml":   `<?xml version="1.0"?><Types></Types>`,
		"ppt/slides/slide1.xml": slideXML,
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

func readPPTXEntry(path, entry string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.Name != entry {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil {
			return nil, closeErr
		}
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, os.ErrNotExist
}
