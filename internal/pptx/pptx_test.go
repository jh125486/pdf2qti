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
	return &distill.DistilledContext{
		SourceID:         "src01",
		Book:             "Systems Programming",
		ModuleName:       "Module 3",
		MaterialOverview: "Read chapter",
		Agenda:           []string{"Topic A", "Topic B", "Topic C"},
		Slides: []distill.Slide{
			{Title: "Topic A", Content: "Point 1\nPoint 2"},
			{Title: "Topic B", Content: "Point 1"},
			{Title: "Topic C", Content: "Point 1\nPoint 2\nPoint 3"},
		},
	}
}

// baseTemplateEntries returns a minimal but structurally valid pptx: 3 named slide layouts
// (Title/Agenda/Content) and one demo slide each for Title/Agenda/Content, wired up via _rels,
// presentation.xml, and presentation.xml.rels. The title slide is deliberately named slide0.xml
// (not slide3.xml) so it doesn't shift the slide-numbering tests below, which assume content
// duplication starts at slide3/slide4 as it did before the title slide existed.
func baseTemplateEntries() map[string][]byte {
	return map[string][]byte{
		"[Content_Types].xml": []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`),
		"ppt/presentation.xml": []byte(`<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst><p:sldId id="255" r:id="rId1"/><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId3"/></p:sldIdLst></p:presentation>`),
		"ppt/_rels/presentation.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide0.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
			`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`),
		"ppt/slideLayouts/slideLayout1.xml": []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Title"/></p:sldLayout>`),
		"ppt/slideLayouts/slideLayout2.xml": []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Agenda"/></p:sldLayout>`),
		"ppt/slideLayouts/slideLayout3.xml": []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Content"/></p:sldLayout>`),
		"ppt/slides/slide0.xml":             []byte(demoSlideXML("Module Title")),
		"ppt/slides/_rels/slide0.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`),
		"ppt/slides/slide1.xml": []byte(demoSlideXML("Agenda")),
		"ppt/slides/_rels/slide1.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout2.xml"/></Relationships>`),
		"ppt/slides/slide2.xml": []byte(demoSlideXML("Slide Title")),
		"ppt/slides/_rels/slide2.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout3.xml"/></Relationships>`),
	}
}

func demoSlideXML(titleText string) string {
	return `<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>` + titleText + `</a:t></a:r></a:p></p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body" idx="11"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
		`</p:spTree></p:cSld></p:sld>`
}

func TestRender_Success(t *testing.T) {
	t.Parallel()

	entries := baseTemplateEntries()
	entries["customXml/item1.xml"] = []byte(`<root>{{.module_name}} - {{.book}}</root>`)

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, entries); err != nil {
		t.Fatal(err)
	}

	err := pptx.Render(templatePath, sampleContext(), "Test University", map[string]string{"module_name": "Module 3"}, outputPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	mustContainAll(t, "custom part", string(outEntries["customXml/item1.xml"]), "Module 3 - Systems Programming")
	mustContainAll(t, "title slide", string(outEntries["ppt/slides/slide0.xml"]), "<a:t>Module 3</a:t>", "<a:t>Test University</a:t>")
	mustContainAll(t, "agenda slide", string(outEntries["ppt/slides/slide1.xml"]), "<a:t>Topic A</a:t>", "<a:t>Topic B</a:t>", "<a:t>Topic C</a:t>")
	// First content slide reuses the prototype part, id=2; slide3/4 are the duplicates.
	mustContainAll(t, "first content slide", string(outEntries["ppt/slides/slide2.xml"]), "<a:t>Topic A</a:t>", "<a:t>Point 1</a:t>", "<a:t>Point 2</a:t>")
	mustContainAll(t, "duplicated slide3", string(outEntries["ppt/slides/slide3.xml"]), "<a:t>Topic B</a:t>")
	mustContainAll(t, "duplicated slide4", string(outEntries["ppt/slides/slide4.xml"]), "<a:t>Topic C</a:t>")
	mustContainAll(t, "content types", string(outEntries["[Content_Types].xml"]), "/ppt/slides/slide3.xml", "/ppt/slides/slide4.xml")

	pres := string(outEntries["ppt/presentation.xml"])
	if got := strings.Count(pres, "<p:sldId "); got != 5 {
		t.Fatalf("expected 5 sldId entries (title+agenda+3 content), got %d: %q", got, pres)
	}

	presRels := string(outEntries["ppt/_rels/presentation.xml.rels"])
	if got := strings.Count(presRels, `Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"`); got != 5 {
		t.Fatalf("expected 5 slide relationships, got %d: %q", got, presRels)
	}
}

func mustContainAll(t *testing.T, label, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Fatalf("%s missing %q: %q", label, needle, haystack)
		}
	}
}

func TestRender_BinaryEntryPassthrough(t *testing.T) {
	t.Parallel()

	entries := baseTemplateEntries()
	entries["docProps/thumb.jpeg"] = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}

	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.pptx")
	outputPath := filepath.Join(dir, "out.pptx")
	if err := writeZip(templatePath, entries); err != nil {
		t.Fatal(err)
	}

	if err := pptx.Render(templatePath, sampleContext(), "", nil, outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	got := outEntries["docProps/thumb.jpeg"]
	want := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if !bytes.Equal(got, want) {
		t.Fatalf("expected binary entry to pass through unchanged, got %v want %v", got, want)
	}
}

func TestRender_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries func() map[string][]byte
		dc      func() *distill.DistilledContext
		wantErr bool
		errLike string
	}{
		{
			name: "bad entry template syntax",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["customXml/item1.xml"] = []byte(`<root>{{.module_name</root>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "parse template entry",
		},
		{
			name: "missing required layout",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slideLayouts/slideLayout2.xml"] = []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Schedule"/></p:sldLayout>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "template missing required slide layout(s)",
		},
		{
			name:    "agenda count out of range",
			entries: baseTemplateEntries,
			dc: func() *distill.DistilledContext {
				dc := sampleContext()
				dc.Agenda = []string{"Only One", "Only Two"}
				return dc
			},
			wantErr: true,
			errLike: "agenda must have between 3 and 8 bullets",
		},
		{
			name: "no slide using agenda layout",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/_rels/slide1.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `no slide using the "Agenda" layout`,
		},
		{
			name: "no slide using content layout",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/_rels/slide2.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `no slide using the "Content" layout`,
		},
		{
			name:    "no slides to render",
			entries: baseTemplateEntries,
			dc: func() *distill.DistilledContext {
				dc := sampleContext()
				dc.Slides = nil
				return dc
			},
			wantErr: true,
			errLike: "no slides to render",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			templatePath := filepath.Join(dir, "template.pptx")
			outputPath := filepath.Join(dir, "out.pptx")
			if err := writeZip(templatePath, tt.entries()); err != nil {
				t.Fatal(err)
			}

			err := pptx.Render(templatePath, tt.dc(), "", map[string]string{"module_name": "Module 3"}, outputPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}

func TestRender_MissingTemplate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := pptx.Render(filepath.Join(dir, "missing.pptx"), sampleContext(), "", nil, filepath.Join(dir, "out.pptx"))
	if err == nil || !strings.Contains(err.Error(), "read pptx template") {
		t.Fatalf("expected read pptx template error, got %v", err)
	}
}

// TestRender_RealTemplate exercises the full pipeline against the actual PowerPoint-authored
// template checked into testdata, which has richer markup (extLst, notesSlides, media, etc.)
// than the minimal fixtures above.
func TestRender_RealTemplate(t *testing.T) {
	t.Parallel()

	dc := sampleContext()
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.pptx")

	if err := pptx.Render("testdata/template.pptx", dc, "Test University", nil, outputPath); err != nil {
		t.Fatalf("render: %v", err)
	}

	outEntries, err := readZip(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"ppt/slides/slide3.xml", "ppt/slides/slide4.xml", "ppt/slides/slide5.xml"} {
		if _, ok := outEntries[name]; !ok {
			t.Fatalf("expected generated slide part %q in output", name)
		}
	}
	if strings.Count(string(outEntries["ppt/presentation.xml"]), "<p:sldId ") != 5 {
		t.Fatalf("expected 5 sldId entries (title+agenda+3 content), got: %q", string(outEntries["ppt/presentation.xml"]))
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
