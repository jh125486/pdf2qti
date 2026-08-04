package pptx_test

import (
	"archive/zip"
	"bytes"
	"fmt"
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
			{Title: "Topic A", Content: "Point 1\nPoint 2", Tag: "ch1"},
			{Title: "Topic B", Content: "Point 1", Tag: "ch1"},
			{Title: "Topic C", Content: "Point 1\nPoint 2\nPoint 3", Tag: "summary"},
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

func mustContainAll(t *testing.T, label, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Fatalf("%s missing %q: %q", label, needle, haystack)
		}
	}
}

// renderFirstContentSlideDC returns a dc func with slide[0]'s content overridden to content, for
// TestRender table entries whose verify closure only needs the resulting first content slide's
// XML rather than the full outEntries map.
func renderFirstContentSlideDC(content string) func() *distill.DistilledContext {
	return func() *distill.DistilledContext {
		dc := sampleContext()
		dc.Slides[0].Content = content
		return dc
	}
}

type renderTestCase struct {
	name           string
	entries        func() map[string][]byte
	rawTemplate    []byte // when set, written verbatim instead of zipping entries()
	noTemplateFile bool   // when set, no file at all is written at templatePath
	dc             func() *distill.DistilledContext
	courseName     string
	outputPath     func(dir string) string // when set, overrides the default dir/out.pptx
	wantErr        bool
	errLike        string
	verify         func(t *testing.T, outEntries map[string][]byte)
}

// renderTestCases builds TestRender's table. Split out from the test function itself to keep
// gocyclo's complexity count on the (trivial) runner, not this literal.
func renderTestCases() []renderTestCase {
	return []renderTestCase{
		{
			name: "success",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["customXml/item1.xml"] = []byte(`<root>{{.module_name}} - {{.book}}</root>`)
				return e
			},
			dc:         sampleContext,
			courseName: "Test University",
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
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

				if got := strings.Count(pres, "<p14:section "); got != 3 {
					t.Fatalf("expected 3 sections (Introduction, ch1, Summary), got %d: %q", got, pres)
				}
				mustContainAll(t, "presentation.xml sections", pres,
					`<p14:section name="Introduction"`, `<p14:section name="ch1"`, `<p14:section name="Summary"`)
				// ch1 groups the two ch1-tagged slides (Topic A + Topic B) into one section, not two.
				if idx := strings.Index(pres, `<p14:section name="ch1"`); idx != -1 {
					end := strings.Index(pres[idx:], "</p14:section>")
					if got := strings.Count(pres[idx:idx+end], "<p14:sldId "); got != 2 {
						t.Fatalf("expected 2 sldIds in the ch1 section, got %d: %q", got, pres[idx:idx+end])
					}
				}
			},
		},
		{
			name: "binary entry passes through unchanged",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["docProps/thumb.jpeg"] = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
				return e
			},
			dc: sampleContext,
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				got := outEntries["docProps/thumb.jpeg"]
				want := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
				if !bytes.Equal(got, want) {
					t.Fatalf("expected binary entry to pass through unchanged, got %v want %v", got, want)
				}
			},
		},
		{
			name: "empty title still produces a valid title run",
			// An empty title bullet makes runsXML's own text-to-render empty, hitting its
			// no-spans-matched-and-nothing-written fallback (as opposed to the normal case where
			// even plain unmatched text still produces a non-empty run via boldRunsXML).
			entries: baseTemplateEntries,
			dc: func() *distill.DistilledContext {
				dc := sampleContext()
				dc.ModuleName = ""
				return dc
			},
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				mustContainAll(t, "empty title run", string(outEntries["ppt/slides/slide0.xml"]), `<a:r><a:rPr lang="en-US" dirty="0"/><a:t></a:t></a:r>`)
			},
		},
		{
			// \[...\] display-math spans (as opposed to \(...\) inline math) are a distinct
			// branch in runsXML's switch; without pandoc's real conversion available (as here —
			// see math_test.go for the pandoc-available path), both bolded and unbolded must
			// degrade to plain escaped text rather than error or drop content.
			name:    "display math falls back to plain text without pandoc",
			entries: baseTemplateEntries,
			dc:      renderFirstContentSlideDC("Point 1\nSee \\[E = mc^2\\] for details and **\\[F = ma\\]** too"),
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				mustContainAll(t, "display math fallback", string(outEntries["ppt/slides/slide2.xml"]), "E = mc^2", "F = ma")
			},
		},
		{
			// Plain "**bold**" with no adjacent math span at all.
			name:    "plain bold text renders a bold run",
			entries: baseTemplateEntries,
			dc:      renderFirstContentSlideDC("Point 1\nThis is **important** text"),
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				contentSlide := string(outEntries["ppt/slides/slide2.xml"])
				mustContainAll(t, "bold run", contentSlide, `<a:rPr lang="en-US" b="1" dirty="0"/><a:t>important</a:t>`)
				mustContainAll(t, "surrounding plain runs", contentSlide, "This is ", " text")
			},
		},
		{
			// Regression: a bold span that mixes math and plain text ("**\(n\)-tuple**", as
			// opposed to a formula bolded entirely on its own) used to leak both "**" markers
			// through as literal asterisks and leave "-tuple" unbolded — see runsXML's doc
			// comment for why (math extraction ran across the whole string before bold-splitting,
			// so the bold span's opening and closing "**" landed in two separate calls that each
			// only ever saw one of the two markers).
			name:    "bold span mixing math and plain text bolds the plain-text portion, not just the math",
			entries: baseTemplateEntries,
			dc:      renderFirstContentSlideDC("Point 1\nAn **\\(n\\)-tuple** is an ordered list."),
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				contentSlide := string(outEntries["ppt/slides/slide2.xml"])
				if strings.Contains(contentSlide, "**") {
					t.Fatalf("literal \"**\" leaked into output: %s", contentSlide)
				}
				mustContainAll(t, "bold plain-text portion of the mixed span", contentSlide, `<a:rPr lang="en-US" b="1" dirty="0"/><a:t>-tuple</a:t>`)
				mustContainAll(t, "unbolded text before and after the bold span", contentSlide, "<a:t>An </a:t>", "is an ordered list.")
			},
		},
		{
			name:    "blank lines between bullets are skipped",
			entries: baseTemplateEntries,
			dc:      renderFirstContentSlideDC("Point 1\n\n\nPoint 2"),
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				contentSlide := string(outEntries["ppt/slides/slide2.xml"])
				// 1 title paragraph + 2 body bullets; blank lines must not produce empty extras.
				if got := strings.Count(contentSlide, "<a:p>"); got != 3 {
					t.Fatalf("expected blank lines to be skipped (3 total paragraphs: title+2 bullets), got %d: %s", got, contentSlide)
				}
				mustContainAll(t, "both bullets present", contentSlide, "<a:t>Point 1</a:t>", "<a:t>Point 2</a:t>")
			},
		},
		{
			name: "unresolvable extra slides are safely ignored",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				// No _rels part at all: slidesByLayoutName's own rels lookup fails and skips it.
				e["ppt/slides/slide50.xml"] = []byte(demoSlideXML("Orphan"))
				// A _rels part with no slideLayout-type relationship in it.
				e["ppt/slides/slide51.xml"] = []byte(demoSlideXML("No Layout Rel"))
				e["ppt/slides/_rels/slide51.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`)
				// A slideLayout relationship whose target doesn't resolve to any known layout part.
				e["ppt/slides/slide52.xml"] = []byte(demoSlideXML("Dangling Layout Target"))
				e["ppt/slides/_rels/slide52.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayoutMissing.xml"/></Relationships>`)
				return e
			},
			dc: sampleContext,
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				// Normal slide numbering must be entirely unaffected by the unresolvable junk slides.
				mustContainAll(t, "title slide", string(outEntries["ppt/slides/slide0.xml"]), "<a:t>Module 3</a:t>")
				mustContainAll(t, "first content slide", string(outEntries["ppt/slides/slide2.xml"]), "<a:t>Topic A</a:t>")
			},
		},
		{
			name: "layout pictures gain descr in every tag form",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slideLayouts/slideLayout1.xml"] = []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Title">` +
					// Self-closing <p:cNvPr .../> with no descr: must gain descr="".
					`<p:pic><p:nvPicPr><p:cNvPr id="1" name="logo1"/></p:nvPicPr></p:pic>` +
					// Open <p:cNvPr ...> tag with no descr: must gain descr="".
					`<p:pic><p:nvPicPr><p:cNvPr id="2" name="logo2"></p:cNvPr></p:nvPicPr></p:pic>` +
					// Already has a descr: left untouched.
					`<p:pic><p:nvPicPr><p:cNvPr id="3" name="logo3" descr="Company logo"/></p:nvPicPr></p:pic>` +
					`</p:cSld></p:sldLayout>`)
				return e
			},
			dc: sampleContext,
			verify: func(t *testing.T, outEntries map[string][]byte) {
				t.Helper()
				layout := string(outEntries["ppt/slideLayouts/slideLayout1.xml"])
				mustContainAll(t, "self-closing tag gains descr", layout, `<p:cNvPr id="1" name="logo1" descr=""/>`)
				mustContainAll(t, "open tag gains descr", layout, `<p:cNvPr id="2" name="logo2" descr="">`)
				mustContainAll(t, "existing descr left untouched", layout, `<p:cNvPr id="3" name="logo3" descr="Company logo"/>`)
				if strings.Count(layout, `descr="Company logo"`) != 1 {
					t.Fatalf("existing descr must not be duplicated: %s", layout)
				}
			},
		},
		{
			name:           "missing template file",
			noTemplateFile: true,
			dc:             sampleContext,
			wantErr:        true,
			errLike:        "read pptx template",
		},
		{
			name:        "template file is not a valid zip",
			rawTemplate: []byte("this is not a zip archive"),
			dc:          sampleContext,
			wantErr:     true,
			errLike:     "open pptx template",
		},
		{
			// A syntactically-valid zip (correct central directory) whose title-slide entry
			// fails its CRC-32 checksum on read — distinct from "not a valid zip" above, which
			// fails earlier at zip.NewReader itself.
			name:        "template entry fails checksum on read",
			rawTemplate: zipBytesWithCorruptEntry(baseTemplateEntries(), "ppt/slides/slide0.xml"),
			dc:          sampleContext,
			wantErr:     true,
			errLike:     `read template entry "ppt/slides/slide0.xml"`,
		},
		{
			name:    "output dir blocked by an existing file",
			entries: baseTemplateEntries,
			dc:      sampleContext,
			outputPath: func(dir string) string {
				blocker := filepath.Join(dir, "blocker")
				if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
					panic(err)
				}
				return filepath.Join(blocker, "out.pptx")
			},
			wantErr: true,
			errLike: "create output dir",
		},
		{
			name:    "output path is an existing directory",
			entries: baseTemplateEntries,
			dc:      sampleContext,
			outputPath: func(dir string) string {
				return dir // dir itself already exists; os.Create on it must fail
			},
			wantErr: true,
			errLike: "create output",
		},
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
			name: "template execute error from bad field access",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["customXml/item1.xml"] = []byte(`<root>{{.module_name.Bogus}}</root>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "execute template entry",
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
		{
			name: "title slide has no title placeholder",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body" idx="11"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `fill title slide "ppt/slides/slide0.xml"`,
		},
		{
			name: "title slide has no body placeholder for courseName",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:         sampleContext,
			courseName: "Test University",
			wantErr:    true,
			errLike:    `fill title slide "ppt/slides/slide0.xml"`,
		},
		{
			name: "slide layout missing name attribute",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slideLayouts/slideLayout1.xml"] = []byte(`<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld/></p:sldLayout>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "has no name attribute",
		},
		{
			name: "no slide using title layout",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/_rels/slide0.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout2.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `no slide using the "Title" layout`,
		},
		{
			name: "presentation rels missing entry for title slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/_rels/presentation.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
					`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `find presentation relationship for "ppt/slides/slide0.xml"`,
		},
		{
			name: "presentation rels missing entry for agenda slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/_rels/presentation.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide0.xml"/>` +
					`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `find presentation relationship for "ppt/slides/slide1.xml"`,
		},
		{
			name: "agenda slide has no body placeholder",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide1.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `fill agenda slide "ppt/slides/slide1.xml"`,
		},
		{
			// A slide missing its _rels part entirely can't be matched to any layout at all
			// (slidesByLayoutName's own rels lookup fails first, skipping it), not just the
			// Content one — duplicateContentSlides never even gets a chance to run its own
			// (separate, always-redundant-in-practice) rels-presence check on the prototype,
			// since a part slidesByLayoutName already resolved is guaranteed to still have its
			// rels part in scope by the time duplicateContentSlides re-reads it.
			name: "slide missing relationships part can't be resolved to any layout",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				delete(e, "ppt/slides/_rels/slide2.xml.rels")
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `no slide using the "Content" layout`,
		},
		{
			name: "presentation rels missing entry for content slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/_rels/presentation.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide0.xml"/>` +
					`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `find presentation relationship for "ppt/slides/slide2.xml"`,
		},
		{
			// Same visible error as the case above, but exercises a different internal branch:
			// the Relationship element DOES match the target, but is missing its Id attribute
			// entirely (relationshipIDForTarget's idm==nil continue), rather than never matching
			// the target at all.
			name: "presentation rels entry for content slide has no Id attribute",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/_rels/presentation.xml.rels"] = []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide0.xml"/>` +
					`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
					`<Relationship Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `find presentation relationship for "ppt/slides/slide2.xml"`,
		},
		{
			name: "presentation.xml sldIdLst missing entry for content slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/presentation.xml"] = []byte(`<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
					`<p:sldIdLst><p:sldId id="255" r:id="rId1"/><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: `no sldId found for r:id "rId3"`,
		},
		{
			name: "content slide has no title placeholder",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide2.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="body" idx="11"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "set title for slide 1",
		},
		{
			name: "content slide has no body placeholder",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide2.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "set content for slide 1",
		},
		{
			name: "Content_Types missing closing tag blocks second content slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["[Content_Types].xml"] = []byte(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></NotTypes>`)
				return e
			},
			dc:      sampleContext, // 3 slides: the 2nd (i=1) is where duplication first appends a part
			wantErr: true,
			errLike: "no </Types> marker",
		},
		{
			name: "presentation rels missing closing tag blocks second content slide",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/_rels/presentation.xml.rels"] = []byte(`<?xml version="1.0"?><NotRelationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide0.xml"/>` +
					`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
					`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></NotRelationships>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "no </Relationships> marker",
		},
		{
			name: "title slide missing enclosing sp",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:ph type="title"/><a:lstStyle/></p:txBody>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "no enclosing <p:sp>",
		},
		{
			name: "title slide missing closing sp",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "no closing </p:sp>",
		},
		{
			name: "title slide missing lstStyle",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "no <a:lstStyle/>",
		},
		{
			name: "title slide missing closing txBody",
			entries: func() map[string][]byte {
				e := baseTemplateEntries()
				e["ppt/slides/slide0.xml"] = []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree>` +
					`<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:sp>` +
					`</p:spTree></p:cSld></p:sld>`)
				return e
			},
			dc:      sampleContext,
			wantErr: true,
			errLike: "no closing </p:txBody>",
		},
	}
}

// TestRender covers pptx.Render (pptx.go's only exported function) end to end: the success path
// and every error branch across template parsing, layout validation, slide duplication, and
// section grouping. Table-driven per this repo's Go test conventions; scenarios needing a real
// on-disk template fixture instead of the synthetic entries below are the one justified
// exception (see TestRender_RealTemplate).
func TestRender(t *testing.T) {
	t.Parallel()

	for _, tt := range renderTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			templatePath := filepath.Join(dir, "template.pptx")
			outputPath := filepath.Join(dir, "out.pptx")
			if tt.outputPath != nil {
				outputPath = tt.outputPath(dir)
			}
			switch {
			case tt.noTemplateFile:
				// intentionally leave templatePath unwritten
			case tt.rawTemplate != nil:
				if err := os.WriteFile(templatePath, tt.rawTemplate, 0o600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := writeZip(templatePath, tt.entries()); err != nil {
					t.Fatal(err)
				}
			}

			err := pptx.Render(templatePath, tt.dc(), tt.courseName, map[string]string{"module_name": "Module 3"}, outputPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if tt.verify != nil {
				outEntries, err := readZip(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				tt.verify(t, outEntries)
			}
		})
	}
}

// TestRender_RealTemplate is a justified exception to the single-table-function convention
// above: it exercises the full pipeline against the actual PowerPoint-authored template checked
// into testdata, which has richer markup (extLst, notesSlides, media, etc.) than the minimal
// synthetic fixtures TestRender builds, and needs its own multi-part verification that doesn't
// reduce to a simple wantErr/errLike/verify-closure table entry.
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
	pres := string(outEntries["ppt/presentation.xml"])
	if strings.Count(pres, "<p:sldId ") != 5 {
		t.Fatalf("expected 5 sldId entries (title+agenda+3 content), got: %q", pres)
	}

	// The real template already has a <p:extLst> (sldGuideLst) — sections must merge into it
	// rather than duplicate the element, and it must still be well-formed (exactly one open/close
	// pair) afterward.
	if got := strings.Count(pres, "<p:extLst>"); got != 1 {
		t.Fatalf("expected exactly 1 <p:extLst>, got %d: %q", got, pres)
	}
	mustContainAll(t, "presentation.xml", pres, "p15:sldGuideLst", "<p14:sectionLst", `<p14:section name="Introduction"`)

	// Every picture in a slide layout should be marked decorative (empty descr), since they're
	// template chrome (logos, background graphics) identical on every generated slide.
	for _, name := range []string{"ppt/slideLayouts/slideLayout1.xml", "ppt/slideLayouts/slideLayout2.xml", "ppt/slideLayouts/slideLayout3.xml"} {
		layout, ok := outEntries[name]
		if !ok {
			continue
		}
		picCount := strings.Count(string(layout), "<p:pic>")
		descrCount := strings.Count(string(layout), `descr=""`)
		if picCount > 0 && descrCount != picCount {
			t.Fatalf("%s: %d pictures but only %d marked decorative", name, picCount, descrCount)
		}
	}

	// Every placeholder setPlaceholderBullets populated with generated text must declare
	// <a:normAutofit/> on its own bodyPr. This testdata template's layouts (unlike the real
	// course template ensureNormAutofit's doc comment describes) don't declare normAutofit at
	// all, so its presence here can only have come from the generator itself — the strongest
	// possible version of this assertion, independent of what any given template's layouts do.
	for _, name := range []string{"ppt/slides/slide3.xml", "ppt/slides/slide4.xml", "ppt/slides/slide5.xml"} {
		slide := string(outEntries[name])
		bodyPrCount := strings.Count(slide, "<a:bodyPr")
		autofitCount := strings.Count(slide, "<a:normAutofit/>")
		if autofitCount != bodyPrCount {
			t.Fatalf("%s: %d bodyPr but only %d with normAutofit", name, bodyPrCount, autofitCount)
		}
	}

	// This real template's viewProps.xml declares lastView="sldMasterView" (its author's last
	// save was in Slide Master view) — Render must force it to "sldView" so a generated deck
	// opens straight to slide 1 in Normal view instead of inheriting the template author's
	// editing view.
	mustContainAll(t, "viewProps.xml", string(outEntries["ppt/viewProps.xml"]), `lastView="sldView"`)
	if strings.Contains(string(outEntries["ppt/viewProps.xml"]), "sldMasterView") {
		t.Fatal("viewProps.xml still declares sldMasterView")
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

// zipBytesWithCorruptEntry builds a zip archive (uncompressed, Store method, for predictable
// byte layout) from entries, then flips one byte inside targetEntry's stored data — without
// updating its CRC-32 header field — so the archive still parses (valid central directory,
// correct sizes) but the corrupted entry fails its CRC check on read. Exercises the
// io.ReadAll(rc) error branches in readParts/extractDocumentXML, which a syntactically-invalid
// zip can't reach (that fails earlier, at zip.NewReader itself).
// zipBytesWithCorruptEntry panics on failure rather than taking a *testing.T: it's called while
// building table-test fixtures, outside any subtest's closure.
func zipBytesWithCorruptEntry(entries map[string][]byte, targetEntry string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			panic(err)
		}
		if _, err := w.Write(body); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	data := buf.Bytes()

	body, ok := entries[targetEntry]
	if !ok {
		panic(fmt.Sprintf("entry %q not found in built zip", targetEntry))
	}
	// Store method means the entry's bytes appear verbatim right after its local file header;
	// locate them by content and flip the first byte, without touching the header's CRC-32
	// field, so the archive still parses but this entry fails its checksum on read.
	idx := bytes.Index(data, body)
	if idx == -1 {
		panic(fmt.Sprintf("could not locate raw bytes of entry %q to corrupt", targetEntry))
	}
	data[idx] ^= 0xFF
	return data
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
