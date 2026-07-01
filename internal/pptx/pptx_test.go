package pptx_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/pptx"
)

func sampleContext() *distill.DistilledContext {
	return &distill.DistilledContext{SourceID: "src01", Book: "Systems Programming", ModuleName: "Module 3", MaterialOverview: "Read chapter"}
}

func TestRender_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) (templatePath, outputPath string)
		wantErr bool
		errLike string
		verify  func(t *testing.T, outputPath string)
	}{
		{
			name: "success",
			prepare: func(t *testing.T, dir string) (string, string) {
				t.Helper()
				templatePath := filepath.Join(dir, "template.pptx")
				outputPath := filepath.Join(dir, "out.pptx")
				entries := map[string][]byte{
					"[Content_Types].xml":             []byte(`<?xml version="1.0"?><Types></Types>`),
					"ppt/slides/slide1.xml":           []byte(`<a:t>{{.module_name}} - {{.book}}</a:t>`),
					"ppt/_rels/presentation.xml.rels": []byte(`<Relationships><Relationship Target="{{.source_id}}"/></Relationships>`),
				}
				if err := writeZip(templatePath, entries); err != nil {
					t.Fatal(err)
				}
				return templatePath, outputPath
			},
			verify: func(t *testing.T, outputPath string) {
				t.Helper()
				outEntries, err := readZip(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				if got := string(outEntries["ppt/slides/slide1.xml"]); !strings.Contains(got, "Module 3 - Systems Programming") {
					t.Fatalf("unexpected rendered slide: %q", got)
				}
			},
		},
		{
			name: "binary entry passthrough",
			prepare: func(t *testing.T, dir string) (string, string) {
				t.Helper()
				templatePath := filepath.Join(dir, "template.pptx")
				outputPath := filepath.Join(dir, "out.pptx")
				binary := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
				entries := map[string][]byte{
					"[Content_Types].xml": []byte(`<?xml version="1.0"?><Types></Types>`),
					"docProps/thumb.jpeg": binary,
				}
				if err := writeZip(templatePath, entries); err != nil {
					t.Fatal(err)
				}
				return templatePath, outputPath
			},
			verify: func(t *testing.T, outputPath string) {
				t.Helper()
				outEntries, err := readZip(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				got := outEntries["docProps/thumb.jpeg"]
				want := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
				if !bytes.Equal(got, want) {
					t.Fatalf("expected binary entry to pass through unchanged, got %v want %v", got, want)
				}
			},
		},
		{
			name: "missing template",
			prepare: func(_ *testing.T, dir string) (string, string) {
				return filepath.Join(dir, "missing.pptx"), filepath.Join(dir, "out.pptx")
			},
			wantErr: true,
			errLike: "read pptx template",
		},
		{
			name: "bad entry template syntax",
			prepare: func(t *testing.T, dir string) (string, string) {
				t.Helper()
				templatePath := filepath.Join(dir, "template.pptx")
				outputPath := filepath.Join(dir, "out.pptx")
				entries := map[string][]byte{
					"[Content_Types].xml":   []byte(`<?xml version="1.0"?><Types></Types>`),
					"ppt/slides/slide1.xml": []byte(`<a:t>{{.module_name</a:t>`),
				}
				if err := writeZip(templatePath, entries); err != nil {
					t.Fatal(err)
				}
				return templatePath, outputPath
			},
			wantErr: true,
			errLike: "parse template entry",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tpl, out := tt.prepare(t, dir)
			err := pptx.Render(tpl, sampleContext(), map[string]string{"module_name": "Module 3"}, out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if tt.verify != nil {
				tt.verify(t, out)
			}
		})
	}
}

func writeZip(path string, entries map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return zw.Close()
}

func readZip(path string) (map[string][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(zr.File))
	for _, file := range zr.File {
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
		out[file.Name] = body
	}
	return out, nil
}
