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
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/jh125486/pdf2qti/internal/distill"
)

// Required slide layout names, validated against the template's ppt/slideLayouts/*.xml parts.
const (
	layoutTitle   = "Title"
	layoutAgenda  = "Agenda"
	layoutContent = "Content"
	layoutDiagram = "Diagram"
)

var (
	reSlideLayoutPart = regexp.MustCompile(`^ppt/slideLayouts/[^/]+\.xml$`)
	reSlidePart       = regexp.MustCompile(`^ppt/slides/(slide(\d+))\.xml$`)
	reCSldName        = regexp.MustCompile(`<p:cSld[^>]*\sname="([^"]*)"`)
	reSlideLayoutRel  = regexp.MustCompile(`Type="[^"]*slideLayout"[^>]*Target="([^"]+)"`)
	reRelationshipEl  = regexp.MustCompile(`<Relationship\b[^>]*/>`)
	reRelIDAttr       = regexp.MustCompile(`\bId="([^"]+)"`)
	reRelTargetAttr   = regexp.MustCompile(`\bTarget="([^"]+)"`)
	reRelIDGlobal     = regexp.MustCompile(`Id="rId(\d+)"`)
	reSldIDGlobal     = regexp.MustCompile(`<p:sldId id="(\d+)"`)
	rePicBlock        = regexp.MustCompile(`(?s)<p:pic>.*?</p:pic>`)
	reCNvPrOpen       = regexp.MustCompile(`<p:cNvPr\b[^>]*>`)
	reLastView        = regexp.MustCompile(`\blastView="[^"]*"`)

	xmlTextReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
)

// markLayoutPicturesDecorative adds descr="" to every <p:cNvPr> inside a <p:pic> in a slide
// layout part that doesn't already have a descr attribute. Layout-level pictures (logos,
// background graphics) are template chrome — identical on every generated slide, never
// content-specific — so marking them decorative for screen readers is correct, and doesn't
// require inventing alt text no caller actually has.
func markLayoutPicturesDecorative(parts map[string][]byte) {
	for name, data := range parts {
		if !reSlideLayoutPart.MatchString(name) {
			continue
		}
		parts[name] = rePicBlock.ReplaceAllFunc(data, func(pic []byte) []byte {
			return reCNvPrOpen.ReplaceAllFunc(pic, addEmptyDescr)
		})
	}
}

// addEmptyDescr adds descr="" to a <p:cNvPr ...> opening tag, unless it already has a descr
// attribute, handling both self-closing (.../>) and open (...>) tag forms.
func addEmptyDescr(tag []byte) []byte {
	if bytes.Contains(tag, []byte("descr=")) {
		return tag
	}
	if bytes.HasSuffix(tag, []byte("/>")) {
		return append(tag[:len(tag)-2:len(tag)-2], []byte(` descr=""/>`)...)
	}
	return append(tag[:len(tag)-1:len(tag)-1], []byte(` descr="">`)...)
}

// resetLastView forces ppt/viewProps.xml's lastView attribute to "sldView" (PowerPoint's Normal
// editing view) if the part is present and declares one. A PPTX template's viewProps.xml records
// whatever view was active when its author last saved the file — commonly "sldMasterView", left
// behind by whoever was last in Slide Master editing a placeholder or master-level setting (e.g.
// the "shrink text on overflow" default) — and PowerPoint faithfully reopens every file in that
// same view. Render otherwise copies this part through unmodified, so every generated deck would
// inherit whatever view the template's author happened to be in, rather than opening straight to
// slide 1 like a normal presentation. A part with no lastView attribute at all, or no viewProps.xml
// part, is left untouched.
func resetLastView(parts map[string][]byte) {
	const viewPropsPart = "ppt/viewProps.xml"
	data, ok := parts[viewPropsPart]
	if !ok {
		return
	}
	parts[viewPropsPart] = reLastView.ReplaceAll(data, []byte(`lastView="sldView"`))
}

// Render reads a PPTX template file, validates Title/Agenda layouts plus Content and Diagram
// layouts needed by dc.Slides, fills in the title slide (dc.ModuleName and courseName), agenda
// bullets, and duplicates Content or Diagram slides once per dc.Slides entry (see
// distill.Slide.Diagram), executes Go text templates in remaining XML/RELS parts against
// distilled context data and vars, and writes result to outputPath.
//
// warnings reports every formula that failed to convert to real OOXML math (see mathWarnings) and
// every diagram that failed to render to a picture (see diagramWarnings), both of which fall back
// to a degraded-but-still-shipped slide rather than failing Render outright, so callers can
// surface them instead of a silent gap in the deck (nil, not an error, on full success).
func Render(templatePath string, dc *distill.DistilledContext, courseName string, vars map[string]string, outputPath string) (warnings []string, err error) {
	inData, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read pptx template %q: %w", templatePath, err)
	}

	reader, err := zip.NewReader(bytes.NewReader(inData), int64(len(inData)))
	if err != nil {
		return nil, fmt.Errorf("open pptx template %q: %w", templatePath, err)
	}

	parts, headers, order, err := readParts(reader)
	if err != nil {
		return nil, err
	}

	markLayoutPicturesDecorative(parts)
	resetLastView(parts)

	mathW := &mathWarnings{}
	diagW := &diagramWarnings{}
	if err := applyDeck(parts, &order, dc, courseName, mathW, diagW); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create output %q: %w", outputPath, err)
	}
	defer outFile.Close()

	writer := zip.NewWriter(outFile)
	closeWriter := true
	defer func() {
		if closeWriter {
			_ = writer.Close()
		}
	}()

	data := buildData(dc, vars)
	for _, name := range order {
		header := headers[name]
		if err := writeEntry(writer, name, parts[name], &header, data); err != nil {
			return nil, err
		}
	}

	closeWriter = false
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finalize output pptx: %w", err)
	}

	return append(mathW.warnings(), diagW.warnings()...), nil
}

// readParts reads every entry of reader into memory, keyed by part name, along with its original
// zip.FileHeader (to preserve compression method etc. on write) and the entries' original order.
func readParts(reader *zip.Reader) (parts map[string][]byte, headers map[string]zip.FileHeader, order []string, err error) {
	parts = make(map[string][]byte, len(reader.File))
	headers = make(map[string]zip.FileHeader, len(reader.File))
	order = make([]string, 0, len(reader.File))

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open template entry %q: %w", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read template entry %q: %w", file.Name, err)
		}
		if closeErr != nil {
			return nil, nil, nil, fmt.Errorf("close template entry %q: %w", file.Name, closeErr)
		}
		parts[file.Name] = data
		headers[file.Name] = file.FileHeader
		order = append(order, file.Name)
	}
	return parts, headers, order, nil
}

// writeEntry writes a single part to writer, running it through the generic text/template pass
// when it's an XML/RELS part, or copying it verbatim otherwise.
func writeEntry(writer *zip.Writer, name string, data []byte, header *zip.FileHeader, tmplData map[string]any) error {
	hdr := *header
	hdr.Name = name
	entryWriter, err := writer.CreateHeader(&hdr)
	if err != nil {
		return fmt.Errorf("create output entry %q: %w", name, err)
	}

	if !isTemplatedPart(name) {
		if _, err := entryWriter.Write(data); err != nil {
			return fmt.Errorf("copy entry %q: %w", name, err)
		}
		return nil
	}

	tmpl, err := template.New(name).Parse(string(data))
	if err != nil {
		return fmt.Errorf("parse template entry %q: %w", name, err)
	}
	if err := tmpl.Execute(entryWriter, tmplData); err != nil {
		return fmt.Errorf("execute template entry %q: %w", name, err)
	}
	return nil
}

func isTemplatedPart(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels")
}

// deckLayoutNeeds reports whether slides contains at least one bullet slide (slide.Diagram ==
// nil) and/or at least one Mermaid diagram slide, so applyDeck/validateLayouts only require
// whichever of the Content/Diagram layouts the deck actually uses.
func deckLayoutNeeds(slides []distill.Slide) (needsContent, needsDiagram bool) {
	for _, slide := range slides {
		if slide.Diagram == nil {
			needsContent = true
		} else {
			needsDiagram = true
		}
	}
	return needsContent, needsDiagram
}

// requiredLayoutSlide looks up layout in slidesByLayout (as built by slidesByLayoutName) and
// errors if it's required (per `required`, e.g. needsContent/needsDiagram) but the template has no
// slide using that layout. required == false always succeeds, returning "" for a layout the
// template happens not to have and the deck doesn't need anyway.
func requiredLayoutSlide(slidesByLayout map[string]string, layout string, required bool) (string, error) {
	slide := slidesByLayout[layout]
	if required && slide == "" {
		return "", fmt.Errorf("template has no slide using the %q layout", layout)
	}
	return slide, nil
}

// applyDeck validates the template's slide layouts and mutates parts/order in place to fill the
// title slide, inject agenda bullets into the Agenda slide, and duplicate the Content and Diagram
// slides once per dc.Slides entry (see duplicateDeckSlides).
func applyDeck(parts map[string][]byte, order *[]string, dc *distill.DistilledContext, courseName string, mathW *mathWarnings, diagW *diagramWarnings) error {
	layoutNames, err := layoutNamesByPart(parts)
	if err != nil {
		return err
	}
	needsContent, needsDiagram := deckLayoutNeeds(dc.Slides)
	if err := validateLayouts(layoutNames, needsContent, needsDiagram); err != nil {
		return err
	}

	slidesByLayout := slidesByLayoutName(parts, *order, layoutNames)

	titleSlide, err := requiredLayoutSlide(slidesByLayout, layoutTitle, true)
	if err != nil {
		return err
	}
	agendaSlide, err := requiredLayoutSlide(slidesByLayout, layoutAgenda, true)
	if err != nil {
		return err
	}
	contentSlide, err := requiredLayoutSlide(slidesByLayout, layoutContent, needsContent)
	if err != nil {
		return err
	}
	diagramSlide, err := requiredLayoutSlide(slidesByLayout, layoutDiagram, needsDiagram)
	if err != nil {
		return err
	}

	// Resolved before any mutation touches ppt/presentation.xml, since sldIDForPart reads the
	// slide's r:id -> numeric sldId mapping directly off it.
	presData := parts["ppt/presentation.xml"]
	titleSldID, err := sldIDForPart(parts, presData, titleSlide)
	if err != nil {
		return err
	}
	agendaSldID, err := sldIDForPart(parts, presData, agendaSlide)
	if err != nil {
		return err
	}

	if err := fillTitleSlide(parts, titleSlide, dc.ModuleName, courseName, mathW); err != nil {
		return err
	}

	if err := fillAgenda(parts, agendaSlide, dc.Agenda, mathW); err != nil {
		return err
	}

	const presRelsPart = "ppt/_rels/presentation.xml.rels"
	agendaRID, err := relationshipIDForTarget(parts[presRelsPart], strings.TrimPrefix(agendaSlide, "ppt/"))
	if err != nil {
		return fmt.Errorf("find presentation relationship for %q: %w", agendaSlide, err)
	}

	contentSldIDs, err := duplicateDeckSlides(parts, order, contentSlide, diagramSlide, agendaRID, dc.Slides, mathW, diagW)
	if err != nil {
		return err
	}

	parts["ppt/presentation.xml"] = addSections(parts["ppt/presentation.xml"], titleSldID, agendaSldID, dc.Slides, contentSldIDs)
	return nil
}

// fillTitleSlide sets the Title-layout slide's title placeholder to title and, if courseName is
// non-empty, its body (subtitle) placeholder to courseName. courseName is left as whatever the
// template's own placeholder text is when empty, rather than erroring, since not every caller
// has a course name to supply.
func fillTitleSlide(parts map[string][]byte, slidePart, title, courseName string, warnings *mathWarnings) error {
	updated, err := setPlaceholderBullets(parts[slidePart], "title", []bulletLine{{text: title}}, nil, warnings)
	if err != nil {
		return fmt.Errorf("fill title slide %q: %w", slidePart, err)
	}
	if courseName != "" {
		updated, err = setPlaceholderBullets(updated, "body", []bulletLine{{text: courseName}}, nil, warnings)
		if err != nil {
			return fmt.Errorf("fill title slide %q: %w", slidePart, err)
		}
	}
	parts[slidePart] = updated
	return nil
}

// layoutNamesByPart returns a map of slide layout part name -> its <p:cSld name="..."> value.
func layoutNamesByPart(parts map[string][]byte) (map[string]string, error) {
	names := make(map[string]string)
	for name, data := range parts {
		if !reSlideLayoutPart.MatchString(name) {
			continue
		}
		m := reCSldName.FindSubmatch(data)
		if m == nil {
			return nil, fmt.Errorf("slide layout %q has no name attribute", name)
		}
		names[name] = string(m[1])
	}
	return names, nil
}

func validateLayouts(layoutNames map[string]string, needsContent, needsDiagram bool) error {
	have := make(map[string]bool, len(layoutNames))
	for _, n := range layoutNames {
		have[n] = true
	}
	var missing []string
	for _, want := range []string{layoutTitle, layoutAgenda} {
		if !have[want] {
			missing = append(missing, want)
		}
	}
	if needsContent && !have[layoutContent] {
		missing = append(missing, layoutContent)
	}
	if needsDiagram && !have[layoutDiagram] {
		missing = append(missing, layoutDiagram)
	}
	if len(missing) > 0 {
		return fmt.Errorf("template missing required slide layout(s): %v", missing)
	}
	return nil
}

// slidesByLayoutName maps each required layout name to the first slide part (in template order)
// that uses it, resolved by following each slide's relationship to its slide layout.
func slidesByLayoutName(parts map[string][]byte, order []string, layoutNames map[string]string) map[string]string {
	result := make(map[string]string)
	for _, name := range order {
		m := reSlidePart.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		layoutPart, ok := slideLayoutPart(parts, name)
		if !ok {
			continue
		}
		layoutName, ok := layoutNames[layoutPart]
		if !ok {
			continue
		}
		if _, exists := result[layoutName]; !exists {
			result[layoutName] = name
		}
	}
	return result
}

// slideLayoutPart resolves slidePart's slide layout part name by following its own relationships
// part.
func slideLayoutPart(parts map[string][]byte, slidePart string) (string, bool) {
	relsData, ok := parts[relsPartFor(slidePart)]
	if !ok {
		return "", false
	}
	rm := reSlideLayoutRel.FindSubmatch(relsData)
	if rm == nil {
		return "", false
	}
	return resolveRelTarget(path.Dir(slidePart), string(rm[1])), true
}

// resolveRelTarget resolves a relationship Target (relative to baseDir) into a normalized part
// path, e.g. resolveRelTarget("ppt/slides", "../slideLayouts/slideLayout2.xml") ->
// "ppt/slideLayouts/slideLayout2.xml".
func resolveRelTarget(baseDir, target string) string {
	return path.Clean(baseDir + "/" + target)
}

// relsPartFor returns the _rels part name for a given part, e.g. "ppt/slides/slide1.xml" ->
// "ppt/slides/_rels/slide1.xml.rels".
func relsPartFor(partName string) string {
	return path.Dir(partName) + "/_rels/" + path.Base(partName) + ".rels"
}

func fillAgenda(parts map[string][]byte, slidePart string, agenda []string, warnings *mathWarnings) error {
	if n := len(agenda); n < 3 || n > 8 {
		return fmt.Errorf("agenda must have between 3 and 8 bullets, got %d", n)
	}
	bullets := make([]bulletLine, len(agenda))
	for i, item := range agenda {
		bullets[i] = bulletLine{text: item}
	}
	updated, err := setPlaceholderBullets(parts[slidePart], "body", bullets, nil, warnings)
	if err != nil {
		return fmt.Errorf("fill agenda slide %q: %w", slidePart, err)
	}
	parts[slidePart] = updated
	return nil
}

// fillContentSlideBody fills prototypeXML's title and body placeholders from slide's title and
// bullet content, with an explicit autofit scale (see estimateAutofitScale) when geom was
// successfully read off the prototype's layout. index is only used to number this slide in an
// error message (1-based, matching the deck's own slide numbering, not slides-of-this-kind
// numbering).
func fillContentSlideBody(prototypeXML []byte, slide distill.Slide, index int, geom bodyGeometry, geomOK bool, warnings *mathWarnings) ([]byte, error) {
	body, err := setPlaceholderBullets(prototypeXML, "title", []bulletLine{{text: slide.Title}}, nil, warnings)
	if err != nil {
		return nil, fmt.Errorf("set title for slide %d: %w", index+1, err)
	}
	bullets := splitBullets(slide.Content)
	var scale *autofitScale
	if geomOK {
		scale = estimateAutofitScale(bullets, geom)
	}
	body, err = setPlaceholderBullets(body, "body", bullets, scale, warnings)
	if err != nil {
		return nil, fmt.Errorf("set content for slide %d: %w", index+1, err)
	}
	return body, nil
}

// deckSlideKind holds everything specific to one of the two prototype slides (Content or Diagram)
// duplicateDeckSlides works with — its part name, whether it's been used in place yet, its
// never-filled XML and original .rels a clone starts from, and the box/geometry its fill step
// needs — so the per-slide loop can pick "the right kind" for a given distill.Slide once and
// operate on it generically, instead of branching on isDiagram at every step.
type deckSlideKind struct {
	proto         string
	used          bool
	origRels      []byte
	pristine      []byte
	contentGeom   bodyGeometry
	contentGeomOK bool
	diagramBox    picBox
}

// newDeckSlideKind captures proto's original .rels and its raw, never-filled XML (before any
// mutation), and, if proto is non-empty, its layout's body geometry or picture-placeholder box
// (whichever isDiagramKind calls for) up front — every duplicate of this kind shares that same
// geometry/box.
//
// pristine is why every clone (see cloneDeckSlide) starts from this snapshot rather than proto's
// current value in parts: fillDiagramSlide structurally REMOVES a Diagram slide's picture
// placeholder (replacePicPlaceholder) on first use, a one-way edit, so a second diagram cloned
// from the first one's already-filled XML would find no placeholder left and fail. A Content
// slide's fill is just a repeatable text replacement either way, so pristine costs it nothing.
func newDeckSlideKind(parts map[string][]byte, proto string, isDiagramKind bool) deckSlideKind {
	k := deckSlideKind{proto: proto, diagramBox: defaultPicBox}
	if proto == "" {
		return k
	}
	k.origRels = append([]byte(nil), parts[relsPartFor(proto)]...)
	k.pristine = append([]byte(nil), parts[proto]...)
	layoutPart, ok := slideLayoutPart(parts, proto)
	if !ok {
		return k
	}
	if isDiagramKind {
		if box, ok := picPlaceholderBox(parts[layoutPart]); ok {
			k.diagramBox = box
		}
		return k
	}
	k.contentGeom, k.contentGeomOK = contentBodyGeometry(parts[layoutPart])
	return k
}

// fillDeckSlide fills slidePart/relsPart (either a prototype being used in place, or a fresh clone
// of one) for slide, dispatching to fillDiagramSlide or fillContentSlideBody depending on which
// kind slide belongs to. index is only used to number the slide in a fillContentSlideBody error
// message.
func fillDeckSlide(parts map[string][]byte, order *[]string, slidePart, relsPart string, slide distill.Slide, index int, kind *deckSlideKind, mediaBySource map[string]string, mediaCounter *int, mathW *mathWarnings, diagW *diagramWarnings) error {
	if slide.Diagram != nil {
		return fillDiagramSlide(parts, order, slidePart, relsPart, slide, mediaBySource, mediaCounter, kind.diagramBox, mathW, diagW)
	}
	body, err := fillContentSlideBody(parts[slidePart], slide, index, kind.contentGeom, kind.contentGeomOK, mathW)
	if err != nil {
		return err
	}
	parts[slidePart] = body
	return nil
}

// slideIDCounters tracks the monotonically increasing numbers duplicateDeckSlides hands out to
// each newly cloned slide part/relationship/sldId, so cloneDeckSlide can claim the next one of
// each without duplicateDeckSlides threading three separate int pointers through every call.
type slideIDCounters struct {
	nextSlideNum int
	nextRID      int
	nextSldID    int
}

// cloneDeckSlide clones kind's pristine, never-filled prototype XML into a brand-new slide part
// positioned right after prevRID in document order: a new slide+rels part (starting from kind's
// original, content-specific-mutation-free .rels), a [Content_Types].xml override, a presentation
// relationship, and a <p:sldId> entry in presData. It returns the clone's part name (for
// fillDeckSlide), its own new sldId (for the caller's sldIDs/Sections bookkeeping) and rID (the
// new prevRID anchor for the next iteration), and presData with the new sldId spliced in.
func cloneDeckSlide(parts map[string][]byte, order *[]string, kind *deckSlideKind, presRelsPart, prevRID string, presData []byte, c *slideIDCounters) (slidePart, rID, sldID string, newPresData []byte, err error) {
	slidePart = fmt.Sprintf("ppt/slides/slide%d.xml", c.nextSlideNum)
	relsPart := relsPartFor(slidePart)
	c.nextSlideNum++

	parts[slidePart] = kind.pristine
	parts[relsPart] = append([]byte(nil), kind.origRels...)
	*order = append(*order, slidePart, relsPart)

	rID = fmt.Sprintf("rId%d", c.nextRID)
	c.nextRID++

	parts["[Content_Types].xml"], err = addContentTypeOverride(parts["[Content_Types].xml"], slidePart)
	if err != nil {
		return "", "", "", nil, err
	}
	parts[presRelsPart], err = addPresentationRelationship(parts[presRelsPart], rID, strings.TrimPrefix(slidePart, "ppt/"))
	if err != nil {
		return "", "", "", nil, err
	}

	sldID = strconv.Itoa(c.nextSldID)
	c.nextSldID++
	newPresData = insertSldIDAfter(presData, prevRID, sldID, rID)
	return slidePart, rID, sldID, newPresData, nil
}

// reusePrototypeInPlace resolves proto's own presentation-relationship id and numeric sldId (it
// already has both, being an existing template slide, unlike a clone) and moves that sldId to sit
// right after prevRID in presData's <p:sldIdLst> — matching the deck's document order regardless
// of where the prototype originally sat in the template.
func reusePrototypeInPlace(parts map[string][]byte, presRelsPart, proto string, presData []byte, prevRID string) (rID, sldID string, newPresData []byte, err error) {
	rID, err = relationshipIDForTarget(parts[presRelsPart], strings.TrimPrefix(proto, "ppt/"))
	if err != nil {
		return "", "", nil, fmt.Errorf("find presentation relationship for %q: %w", proto, err)
	}
	sldID, err = sldIDForRID(presData, rID)
	if err != nil {
		return "", "", nil, err
	}
	return rID, sldID, moveSldIDAfter(presData, prevRID, rID), nil
}

// duplicateDeckSlides fills the Content and Diagram prototype slides with dc.Slides in document
// order — bullet slides (slide.Diagram == nil) against contentProto, mermaid-diagram slides
// against diagramProto — reusing whichever prototype hasn't been used yet in place, and cloning it
// (via cloneDeckSlide) every time that prototype is needed again. Whichever prototype (Content or
// Diagram) never ends up used at all is deleted afterward (removeSlide), so a deck with no diagram
// slides never ships a stray, empty Diagram-layout slide.
//
// agendaRID is the agenda slide's own presentation-relationship id — the anchor prevRID starts
// from, so the first prototype slide filled in place gets moved to sit right after the agenda (see
// reusePrototypeInPlace). It returns each entry's final numeric sldId, in slides order, so callers
// can group them into PowerPoint Sections afterward — the same contract this generalizes from
// (duplicateContentSlides, this function's single-prototype predecessor).
func duplicateDeckSlides(parts map[string][]byte, order *[]string, contentProto, diagramProto, agendaRID string, slides []distill.Slide, mathW *mathWarnings, diagW *diagramWarnings) ([]string, error) {
	if len(slides) == 0 {
		return nil, errors.New("distilled context has no slides to render")
	}

	const presRelsPart = "ppt/_rels/presentation.xml.rels"
	contentKind := newDeckSlideKind(parts, contentProto, false)
	diagramKind := newDeckSlideKind(parts, diagramProto, true)
	kinds := map[bool]*deckSlideKind{false: &contentKind, true: &diagramKind}

	presData := parts["ppt/presentation.xml"]
	counters := &slideIDCounters{
		nextSlideNum: maxSlideNumber(*order) + 1,
		nextRID:      maxRelID(parts[presRelsPart]) + 1,
		nextSldID:    maxSldID(presData) + 1,
	}

	mediaBySource := make(map[string]string)
	mediaCounter := 0
	prevRID := agendaRID
	sldIDs := make([]string, len(slides))

	for i, slide := range slides {
		isDiagram := slide.Diagram != nil
		kind := kinds[isDiagram]
		if kind.proto == "" {
			return nil, fmt.Errorf("template has no slide using the %q layout", map[bool]string{true: layoutDiagram, false: layoutContent}[isDiagram])
		}

		var (
			slidePart, relsPart, rID, sldID string
			err                             error
		)
		if !kind.used {
			slidePart, relsPart = kind.proto, relsPartFor(kind.proto)
			kind.used = true
			rID, sldID, presData, err = reusePrototypeInPlace(parts, presRelsPart, kind.proto, presData, prevRID)
		} else {
			slidePart, rID, sldID, presData, err = cloneDeckSlide(parts, order, kind, presRelsPart, prevRID, presData, counters)
			relsPart = relsPartFor(slidePart)
		}
		if err != nil {
			return nil, err
		}

		if err := fillDeckSlide(parts, order, slidePart, relsPart, slide, i, kind, mediaBySource, &mediaCounter, mathW, diagW); err != nil {
			return nil, err
		}

		sldIDs[i] = sldID
		prevRID = rID
	}

	parts["ppt/presentation.xml"] = presData

	for _, kind := range kinds {
		if kind.used || kind.proto == "" {
			continue
		}
		newPresData, err := removeSlide(parts, order, parts["ppt/presentation.xml"], kind.proto)
		if err != nil {
			return nil, err
		}
		parts["ppt/presentation.xml"] = newPresData
	}

	return sldIDs, nil
}

// nextUnusedMediaPart claims the next "ppt/media/diagramN.png" name not already present in parts
// — a valid template can already ship its own ppt/media/diagram1.png (used by some other slide or
// layout), so a naive always-start-at-1 counter would silently overwrite that unrelated media part
// the first time this deck renders a diagram. *counter is advanced past every name tried,
// including ones skipped for colliding, so a deck's Nth diagram never retries a name an earlier
// diagram in the same Render call already claimed.
func nextUnusedMediaPart(parts map[string][]byte, counter *int) string {
	for {
		*counter++
		candidate := fmt.Sprintf("ppt/media/diagram%d.png", *counter)
		if _, exists := parts[candidate]; !exists {
			return candidate
		}
	}
}

// fillDiagramSlide fills slidePart's title and caption placeholders from slide (unconditionally),
// then attempts to render slide.Diagram's mermaid source to a PNG and embed it as the slide's
// picture. Rendering never hard-fails Render: if mmdc is unavailable or the source fails to
// render, the slide keeps its title and caption but its picture placeholder is left untouched, and
// the failure is recorded on diagW instead (see mermaid.go's file doc comment for the fallback
// contract). mediaBySource/mediaCounter dedupe media parts by mermaid source across the whole
// deck: a diagram repeated verbatim on two slides shares one ppt/media part, each slide getting
// its own relationship pointing at it. box is the picture placeholder's layout-level
// position/size (see picPlaceholderBox/defaultPicBox), used to fit the PNG's actual pixel
// dimensions into that box without upscaling (see fitBox).
func fillDiagramSlide(parts map[string][]byte, order *[]string, slidePart, relsPart string, slide distill.Slide, mediaBySource map[string]string, mediaCounter *int, box picBox, mathW *mathWarnings, diagW *diagramWarnings) error {
	body, err := setPlaceholderBullets(parts[slidePart], "title", []bulletLine{{text: slide.Title}}, nil, mathW)
	if err != nil {
		return fmt.Errorf("fill diagram slide %q: %w", slidePart, err)
	}
	body, err = setPlaceholderBullets(body, "body", []bulletLine{{text: slide.Diagram.Caption}}, nil, mathW)
	if err != nil {
		return fmt.Errorf("fill diagram slide %q: %w", slidePart, err)
	}
	parts[slidePart] = body

	png, err := defaultMermaidRenderer.renderPNG(slide.Diagram.Source)
	if err != nil {
		diagW.add(slide.Title, slide.Diagram.Source, err)
		return nil
	}

	mediaPart, ok := mediaBySource[slide.Diagram.Source]
	if !ok {
		mediaPart = nextUnusedMediaPart(parts, mediaCounter)
		mediaBySource[slide.Diagram.Source] = mediaPart
		parts[mediaPart] = png
		*order = append(*order, mediaPart)

		ct, err := addPNGDefaultIfMissing(parts["[Content_Types].xml"])
		if err != nil {
			return err
		}
		parts["[Content_Types].xml"] = ct
	}

	natW, natH, err := pngDimensions(png)
	if err != nil {
		diagW.add(slide.Title, slide.Diagram.Source, err)
		return nil
	}
	offX, offY, cx, cy := fitBox(natW, natH, box)

	relID := fmt.Sprintf("rId%d", maxRelID(parts[relsPart])+1)
	relTarget := "../media/" + path.Base(mediaPart)
	newRels, err := addImageRelationship(parts[relsPart], relID, relTarget)
	if err != nil {
		return err
	}
	parts[relsPart] = newRels

	picID := maxCNvPrID(parts[slidePart]) + 1
	pic := picXML(picID, slide.Diagram.Alt, relID, picBox{x: offX, y: offY, cx: cx, cy: cy})
	newSlideXML, ok := replacePicPlaceholder(parts[slidePart], pic)
	if !ok {
		return fmt.Errorf("diagram slide %q layout has no picture placeholder", slidePart)
	}
	parts[slidePart] = newSlideXML
	return nil
}

// rePicPhTag matches a <p:ph .../> or <p:ph ...> tag with a type="pic" attribute, anywhere in the
// tag (PowerPoint can order p:ph attributes either way) — see picPlaceholderShapeBounds for why
// anchoring on this tag, not a bare "type=\"pic\"" substring, matters.
var rePicPhTag = regexp.MustCompile(`<p:ph\b[^>]*\btype="pic"[^>]*/?>`)

// picPlaceholderShapeBounds finds the byte range [start, end) of the single <p:sp> shape
// enclosing a <p:ph .../> with type="pic" in xml, anchored on the ph tag's own position — the
// same LastIndex-back/Index-forward scan setPlaceholderBullets uses for a text placeholder,
// applied here to a picture one instead. This deliberately avoids a single DOTALL regex spanning
// "<p:sp>...type=\"pic\"...</p:sp>": Go's RE2 has no lookahead to stop that pattern's lazy `.*?`
// from crossing a sibling shape's boundary, so on a layout/slide whose picture placeholder isn't
// the FIRST <p:sp> in the tree (title and body shapes precede it, as in a real Diagram layout),
// that regex's leftmost match starts at the first sibling's own <p:sp> and swallows every shape up
// to the picture one's close tag — silently deleting the title/body shapes it was never supposed
// to touch. ok is false if no picture placeholder is found.
func picPlaceholderShapeBounds(xml []byte) (start, end int, ok bool) {
	// Anchored on the <p:ph ...> tag itself, not a bare "type=\"pic\"" substring search: this runs
	// on a slide whose title/caption text has already been filled in (fillDiagramSlide fills text
	// first, then swaps the picture placeholder), so a bare substring search could match a literal
	// "type=\"pic\"" sitting inside a diagram's title or caption <a:t> text instead of the real
	// placeholder tag, and drop that text shape instead. rePicPhTag requires the match to start
	// with "<p:ph", which text content can never produce — xmlTextReplacer escapes every literal
	// "<" in inserted text to "&lt;". PowerPoint can order p:ph attributes either way, so
	// type="pic" is still matched anywhere within the tag, not only right after "<p:ph".
	loc := rePicPhTag.FindIndex(xml)
	if loc == nil {
		return 0, 0, false
	}
	phIdx := loc[0]
	spStart := bytes.LastIndex(xml[:phIdx], []byte("<p:sp>"))
	if spStart == -1 {
		return 0, 0, false
	}
	spEndRel := bytes.Index(xml[phIdx:], []byte("</p:sp>"))
	if spEndRel == -1 {
		return 0, 0, false
	}
	return spStart, phIdx + spEndRel + len("</p:sp>"), true
}

// rePicXfrm extracts a shape's explicit position/size from its <a:xfrm>.
var rePicXfrm = regexp.MustCompile(`<a:xfrm>\s*<a:off x="(\d+)" y="(\d+)"/>\s*<a:ext cx="(\d+)" cy="(\d+)"/>\s*</a:xfrm>`)

// defaultPicBox is a conservative 16:9 diagram area used when a Diagram layout's picture
// placeholder omits explicit geometry. Authors should provide that placeholder so their template
// controls exact placement.
var defaultPicBox = picBox{x: 685800, y: 1371600, cx: 10820400, cy: 4114800}

// picPlaceholderBox gets a Diagram layout picture placeholder's explicit geometry. The slide
// placeholder is replaced by a p:pic at this same box during rendering.
func picPlaceholderBox(layoutXML []byte) (picBox, bool) {
	start, end, ok := picPlaceholderShapeBounds(layoutXML)
	if !ok {
		return picBox{}, false
	}
	m := rePicXfrm.FindSubmatch(layoutXML[start:end])
	if m == nil {
		return picBox{}, false
	}
	values := [4]int64{}
	for i := range values {
		n, err := strconv.ParseInt(string(m[i+1]), 10, 64)
		if err != nil || n <= 0 && i >= 2 {
			return picBox{}, false
		}
		values[i] = n
	}
	return picBox{x: values[0], y: values[1], cx: values[2], cy: values[3]}, true
}

// replacePicPlaceholder swaps a Diagram slide's picture placeholder shape for picElement.
func replacePicPlaceholder(slideXML []byte, picElement string) ([]byte, bool) {
	start, end, ok := picPlaceholderShapeBounds(slideXML)
	if !ok {
		return slideXML, false
	}
	out := make([]byte, 0, len(slideXML)-(end-start)+len(picElement))
	out = append(out, slideXML[:start]...)
	out = append(out, picElement...)
	out = append(out, slideXML[end:]...)
	return out, true
}

// reCNvPrID matches any <p:cNvPr id="..."> attribute in a slide, for maxCNvPrID.
var reCNvPrID = regexp.MustCompile(`<p:cNvPr\b[^>]*\bid="(\d+)"`)

// maxCNvPrID returns the highest id="..." on any <p:cNvPr> in slideXML, 0 if none — so a newly
// inserted <p:pic>'s own <p:cNvPr id="..."> can pick one that's guaranteed unique within the
// slide's spTree.
func maxCNvPrID(slideXML []byte) int {
	maxID := 0
	for _, m := range reCNvPrID.FindAllSubmatch(slideXML, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID
}

// addImageRelationship appends an image-type <Relationship> to data (a slide's own .rels part),
// pointing at target (a path relative to ppt/slides/, e.g. "../media/diagram1.png").
func addImageRelationship(data []byte, id, target string) ([]byte, error) {
	if !bytes.Contains(data, []byte("</Relationships>")) {
		return nil, fmt.Errorf("add image relationship %q: no </Relationships> marker", id)
	}
	entry := fmt.Sprintf(`<Relationship Id=%q Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target=%q/>`, id, target)
	return bytes.Replace(data, []byte("</Relationships>"), append([]byte(entry), []byte("</Relationships>")...), 1), nil
}

// reHasPNGDefault detects an existing <Default .../> for the png extension in
// [Content_Types].xml, so addPNGDefaultIfMissing doesn't add a duplicate one when a template
// already declares it. Extension="png" is matched anywhere in the element rather than only
// immediately after <Default, so attribute order (e.g.
// <Default ContentType="image/png" Extension="png"/>) doesn't matter — this still assumes the
// double-quoted, no-surrounding-space form every OOXML producer this package has seen emits, not
// every attribute syntax XML itself permits (single quotes, spaces around "=").
var reHasPNGDefault = regexp.MustCompile(`<Default\b[^>]*\bExtension="[Pp][Nn][Gg]"`)

// addPNGDefaultIfMissing adds a <Default Extension="png" ContentType="image/png"/> to
// [Content_Types].xml if it doesn't already declare one — this package's own testdata template
// has no png parts at all before this feature, so it needs one the first time a diagram is
// rendered; a template that already ships other PNG images is left alone.
func addPNGDefaultIfMissing(data []byte) ([]byte, error) {
	if reHasPNGDefault.Match(data) {
		return data, nil
	}
	if !bytes.Contains(data, []byte("</Types>")) {
		return nil, errors.New("add png content-type default: [Content_Types].xml has no </Types> marker")
	}
	entry := []byte(`<Default Extension="png" ContentType="image/png"/>`)
	return bytes.Replace(data, []byte("</Types>"), append(entry, []byte("</Types>")...), 1), nil
}

// removeSlide deletes an unused prototype slide part (and its _rels part) from parts and order,
// plus its [Content_Types].xml Override, its ppt/_rels/presentation.xml.rels Relationship, and its
// ppt/presentation.xml <p:sldId> entry, so it doesn't ship as a stray, empty slide (see
// duplicateDeckSlides — this is what happens to whichever of Content/Diagram never got used). It
// also strips that numeric sldId out of any PowerPoint Section a template already shipped in its
// own <p14:sectionLst> (see removeDanglingSectionSldID) — addSections (sections.go) only ever
// appends a fresh sectionLst for this render's own slides, it never touches a pre-existing one, so
// a template-authored section referencing the slide being deleted here would otherwise dangle.
// presData is threaded through explicitly, like the rest of this package's slide-mutation
// helpers, rather than re-read from parts, since a caller iterating over multiple removals has a
// more current in-memory copy than what's already been written back into parts.
func removeSlide(parts map[string][]byte, order *[]string, presData []byte, slidePart string) ([]byte, error) {
	relsPart := relsPartFor(slidePart)

	rID, err := relationshipIDForTarget(parts["ppt/_rels/presentation.xml.rels"], strings.TrimPrefix(slidePart, "ppt/"))
	if err != nil {
		return nil, fmt.Errorf("remove unused slide %q: %w", slidePart, err)
	}

	delete(parts, slidePart)
	delete(parts, relsPart)
	*order = removeFromOrder(*order, slidePart, relsPart)

	ctOverride := regexp.MustCompile(`<Override PartName="/` + regexp.QuoteMeta(slidePart) + `"[^>]*/>`)
	parts["[Content_Types].xml"] = ctOverride.ReplaceAll(parts["[Content_Types].xml"], nil)

	relEl := regexp.MustCompile(`<Relationship\b[^>]*\bId="` + regexp.QuoteMeta(rID) + `"[^>]*/>`)
	parts["ppt/_rels/presentation.xml.rels"] = relEl.ReplaceAll(parts["ppt/_rels/presentation.xml.rels"], nil)

	sldIDEl := regexp.MustCompile(`<p:sldId id="(\d+)" r:id="` + regexp.QuoteMeta(rID) + `"/>`)
	m := sldIDEl.FindSubmatch(presData)
	presData = sldIDEl.ReplaceAll(presData, nil)
	if m != nil {
		presData = removeDanglingSectionSldID(presData, string(m[1]))
	}
	return presData, nil
}

// reEmptySection matches a <p14:section> left with no <p14:sldId> children at all, the shape
// removeDanglingSectionSldID's own reference-stripping can produce — an empty section is exactly
// as invalid to PowerPoint as a dangling reference, so it's dropped too, not left behind.
var reEmptySection = regexp.MustCompile(`<p14:section\b[^>]*>\s*<p14:sldIdLst>\s*</p14:sldIdLst>\s*</p14:section>`)

// removeDanglingSectionSldID strips any "<p14:sldId id=\"sldID\"/>" reference to a just-deleted
// slide out of presData's PowerPoint Sections, then drops any section left with no slides at all
// as a result — see removeSlide's doc comment for why this exists at all.
func removeDanglingSectionSldID(presData []byte, sldID string) []byte {
	ref := regexp.MustCompile(`<p14:sldId id="` + regexp.QuoteMeta(sldID) + `"\s*/>`)
	presData = ref.ReplaceAll(presData, nil)
	return reEmptySection.ReplaceAll(presData, nil)
}

// removeFromOrder returns order with every name in names removed, preserving the relative order of
// everything else.
func removeFromOrder(order []string, names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	out := order[:0]
	for _, n := range order {
		if !drop[n] {
			out = append(out, n)
		}
	}
	return out
}

// sldIDForPart resolves partName's numeric <p:sldId id="..."> in presData, by following its
// relationship in ppt/_rels/presentation.xml.rels.
func sldIDForPart(parts map[string][]byte, presData []byte, partName string) (string, error) {
	const presRelsPart = "ppt/_rels/presentation.xml.rels"
	target := strings.TrimPrefix(partName, "ppt/")
	rID, err := relationshipIDForTarget(parts[presRelsPart], target)
	if err != nil {
		return "", fmt.Errorf("find presentation relationship for %q: %w", partName, err)
	}
	return sldIDForRID(presData, rID)
}

// sldIDForRID resolves the numeric <p:sldId id="..."> whose r:id matches rID.
func sldIDForRID(presData []byte, rID string) (string, error) {
	re := regexp.MustCompile(`<p:sldId id="(\d+)" r:id="` + regexp.QuoteMeta(rID) + `"/>`)
	m := re.FindSubmatch(presData)
	if m == nil {
		return "", fmt.Errorf("no sldId found for r:id %q", rID)
	}
	return string(m[1]), nil
}

func relationshipIDForTarget(data []byte, target string) (string, error) {
	for _, el := range reRelationshipEl.FindAll(data, -1) {
		tm := reRelTargetAttr.FindSubmatch(el)
		if len(tm) < 2 || string(tm[1]) != target {
			continue
		}
		idm := reRelIDAttr.FindSubmatch(el)
		if idm == nil {
			continue
		}
		return string(idm[1]), nil
	}
	return "", fmt.Errorf("no relationship found for target %q", target)
}

func maxSlideNumber(order []string) int {
	maxNum := 0
	for _, name := range order {
		m := reSlidePart.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err == nil && n > maxNum {
			maxNum = n
		}
	}
	return maxNum
}

func maxRelID(data []byte) int {
	maxID := 0
	for _, m := range reRelIDGlobal.FindAllSubmatch(data, -1) {
		n, err := strconv.Atoi(string(m[1]))
		if err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID
}

func maxSldID(data []byte) int {
	maxID := 0
	for _, m := range reSldIDGlobal.FindAllSubmatch(data, -1) {
		n, err := strconv.Atoi(string(m[1]))
		if err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID
}

func addContentTypeOverride(data []byte, slidePartName string) ([]byte, error) {
	if !bytes.Contains(data, []byte("</Types>")) {
		return nil, fmt.Errorf("add content-type override for %q: %q has no </Types> marker", slidePartName, "[Content_Types].xml")
	}
	entry := fmt.Sprintf(`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, slidePartName)
	return bytes.Replace(data, []byte("</Types>"), append([]byte(entry), []byte("</Types>")...), 1), nil
}

func addPresentationRelationship(data []byte, rID, target string) ([]byte, error) {
	if !bytes.Contains(data, []byte("</Relationships>")) {
		return nil, fmt.Errorf("add presentation relationship %q: presentation.xml.rels has no </Relationships> marker", rID)
	}
	entry := fmt.Sprintf(`<Relationship Id=%q Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target=%q/>`, rID, target)
	return bytes.Replace(data, []byte("</Relationships>"), append([]byte(entry), []byte("</Relationships>")...), 1), nil
}

// insertSldIDAfter inserts a new <p:sldId> element immediately after the one whose r:id matches
// afterRID, falling back to appending before </p:sldIdLst> if it isn't found.
func insertSldIDAfter(data []byte, afterRID, newID, newRID string) []byte {
	entry := fmt.Sprintf(`<p:sldId id=%q r:id=%q/>`, newID, newRID)
	return insertSldIDElementAfter(data, afterRID, entry)
}

// insertSldIDElementAfter splices the literal element string entry into data immediately after the
// <p:sldId> element whose r:id matches afterRID, falling back to appending before </p:sldIdLst> if
// it isn't found. Factored out of insertSldIDAfter so moveSldIDAfter can reuse the same splicing
// logic for an existing element's exact bytes, rather than a freshly-formatted one.
func insertSldIDElementAfter(data []byte, afterRID, entry string) []byte {
	marker := fmt.Appendf(nil, `r:id=%q/>`, afterRID)
	idx := bytes.Index(data, marker)
	if idx == -1 {
		return bytes.Replace(data, []byte("</p:sldIdLst>"), append([]byte(entry), []byte("</p:sldIdLst>")...), 1)
	}

	insertPos := idx + len(marker)
	out := make([]byte, 0, len(data)+len(entry))
	out = append(out, data[:insertPos]...)
	out = append(out, entry...)
	out = append(out, data[insertPos:]...)
	return out
}

// moveSldIDAfter moves the existing <p:sldId .../> element whose r:id matches moveRID to sit
// immediately after the element whose r:id matches afterRID — a no-op (byte for byte) if it's
// already there, since removing and reinserting an element already in that position reproduces the
// identical bytes. Used when a prototype slide (Content or Diagram) is filled in place rather than
// cloned: its existing sldId entry needs to move into the deck's actual document order, which may
// not match wherever that prototype happened to sit in the original template.
func moveSldIDAfter(presData []byte, afterRID, moveRID string) []byte {
	moveRe := regexp.MustCompile(`<p:sldId id="\d+" r:id="` + regexp.QuoteMeta(moveRID) + `"/>`)
	loc := moveRe.FindIndex(presData)
	if loc == nil {
		return presData
	}
	entry := string(presData[loc[0]:loc[1]])

	without := make([]byte, 0, len(presData)-len(entry))
	without = append(without, presData[:loc[0]]...)
	without = append(without, presData[loc[1]:]...)

	return insertSldIDElementAfter(without, afterRID, entry)
}

// reAutofitChild matches any of the three mutually-exclusive OOXML text-autofit child elements a
// <a:bodyPr> can declare: normAutofit ("shrink text on overflow"), noAutofit, or spAutoFit
// ("resize shape to fit text").
var reAutofitChild = regexp.MustCompile(`<a:(?:normAutofit|noAutofit|spAutoFit)\b`)

// reNormAutofit matches a complete "<a:normAutofit.../>" element (always self-closing — it has no
// children in the schema), for ensureNormAutofit to replace in place when a fresher scale is
// available, rather than only ever detecting its presence.
var reNormAutofit = regexp.MustCompile(`<a:normAutofit\b[^>]*/>`)

// rePrstTxWarp matches a complete <a:prstTxWarp> element, self-closing or with children (e.g. an
// <a:avLst> of adjustment values) — CT_TextBodyProperties's schema requires prstTxWarp, when
// present, to precede the autofit choice, so ensureNormAutofit must insert normAutofit after it
// rather than as bodyPr's first child.
var rePrstTxWarp = regexp.MustCompile(`(?s)^<a:prstTxWarp\b(?:[^>]*/>|[^>]*>.*?</a:prstTxWarp>)`)

// bodyGeometry is a content-slide body placeholder's layout-level box size and default text
// formatting, as read off the slide layout (not the slide itself, which normally leaves these
// unset and inherits) — everything estimateAutofitScale needs to guess how much a slide's actual
// bullet text will need to shrink to fit.
type bodyGeometry struct {
	widthEMU           int
	heightEMU          int
	fontSizeHundredths int // OOXML sz units: hundredths of a point (3600 = 36pt)
	lineSpacePermille  int // OOXML spcPct units: thousandths of a percent (150000 = 150%)
}

var (
	rePlaceholderExt = regexp.MustCompile(`<a:ext cx="(\d+)" cy="(\d+)"/>`)
	reDefRPrSize     = regexp.MustCompile(`<a:defRPr sz="(\d+)"`)
	reLineSpacePct   = regexp.MustCompile(`<a:lnSpc><a:spcPct val="(\d+)"/></a:lnSpc>`)
)

// contentBodyGeometry extracts bodyGeometry from a slide layout's "body" placeholder shape — the
// same shape setPlaceholderBullets locates to write bullets into, read here instead for its
// layout-level box definition and default paragraph properties. ok is false if the layout has no
// such placeholder, or is missing the box size (the one piece estimateAutofitScale can't proceed
// without); a missing font size or line spacing falls back to a reasonable default instead of
// failing outright, since either is a smaller error to absorb than skipping autofit entirely.
func contentBodyGeometry(layoutXML []byte) (bodyGeometry, bool) {
	marker := []byte(`<p:ph type="body"`)
	phIdx := bytes.Index(layoutXML, marker)
	if phIdx == -1 {
		return bodyGeometry{}, false
	}
	spStart := bytes.LastIndex(layoutXML[:phIdx], []byte("<p:sp>"))
	spEndRel := bytes.Index(layoutXML[phIdx:], []byte("</p:sp>"))
	if spStart == -1 || spEndRel == -1 {
		return bodyGeometry{}, false
	}
	block := layoutXML[spStart : phIdx+spEndRel+len("</p:sp>")]

	extM := rePlaceholderExt.FindSubmatch(block)
	if extM == nil {
		return bodyGeometry{}, false
	}
	widthEMU, err1 := strconv.Atoi(string(extM[1]))
	heightEMU, err2 := strconv.Atoi(string(extM[2]))
	if err1 != nil || err2 != nil || widthEMU <= 0 || heightEMU <= 0 {
		return bodyGeometry{}, false
	}

	const defaultFontSizeHundredths = 1800 // 18pt, a conservative fallback if lstStyle omits sz
	fontSizeHundredths := defaultFontSizeHundredths
	if m := reDefRPrSize.FindSubmatch(block); m != nil {
		if v, err := strconv.Atoi(string(m[1])); err == nil && v > 0 {
			fontSizeHundredths = v
		}
	}

	const defaultLineSpacePermille = 100000 // 100%, if lnSpc is unset
	lineSpacePermille := defaultLineSpacePermille
	if m := reLineSpacePct.FindSubmatch(block); m != nil {
		if v, err := strconv.Atoi(string(m[1])); err == nil && v > 0 {
			lineSpacePermille = v
		}
	}

	return bodyGeometry{widthEMU, heightEMU, fontSizeHundredths, lineSpacePermille}, true
}

// reMultiRowMathEnvs matches a LaTeX environment that renders as several stacked rows regardless
// of whether it's wrapped in inline "\(...\)" or display "\[...\]" delimiters — a matrix
// (bmatrix/pmatrix/vmatrix/matrix/cases) or a system of aligned equations (aligned/align) —
// observed in practice: the model routinely puts one of these inside "\(...\)" (not just "\[...\]"
// display math), so delimiter type alone can't be used to detect multi-row content. RE2 has no
// backreferences, so this can't require the \end name to match \begin's the way
// repairMatrixRowSeparators's reMatrixEnvBegin/End pair works around the same limitation; matching
// each environment name as its own alternative, with no cross-checking of the closing tag's name,
// is an acceptable looseness here since this only feeds a line-count estimate, not a correctness-
// critical rewrite.
var reMultiRowMathEnvs = regexp.MustCompile(`(?s)\\begin\{(?:bmatrix|pmatrix|vmatrix|Bmatrix|Pmatrix|Vmatrix|matrix|cases|aligned|align\*?)\}(.*?)\\end\{(?:bmatrix|pmatrix|vmatrix|Bmatrix|Pmatrix|Vmatrix|matrix|cases|aligned|align\*?)\}`)

// countMathRows returns the tallest rendered row count among text's multi-row LaTeX environments
// (see reMultiRowMathEnvs) — 1 if it contains none — by counting "\\" row separators inside each
// plus one. estimateAutofitScale uses this as a floor under its char-count-based line estimate,
// which has no visibility into a matrix or aligned-equations block rendering as several stacked
// rows no matter how short its LaTeX source is.
func countMathRows(text string) int {
	rows := 1
	for _, m := range reMultiRowMathEnvs.FindAllStringSubmatch(text, -1) {
		if n := strings.Count(m[1], `\\`) + 1; n > rows {
			rows = n
		}
	}
	return rows
}

// estimateAutofitScale computes an explicit <a:normAutofit> fontScale/lnSpcReduction for bullets
// laid out in geom's box — or nil if the content fits at 100% and no explicit scale is needed.
//
// A bare "<a:normAutofit/>" (no percentage attributes) only tells a renderer the placeholder
// *wants* autofit; PowerPoint's desktop app recalculates and applies the actual shrink live when
// it renders a box like that, but not every PPTX viewer does the same (observed in practice:
// generated decks opened still overflowing their placeholder despite normAutofit being present).
// Baking in a real, pre-computed scale guarantees correct sizing regardless of whether the viewer
// recalculates autofit itself.
//
// This is a text-layout approximation, not a replica of PowerPoint's own layout engine — there's
// no font-metrics/text-shaping library involved, just two calibrated constants
// (avgCharWidthFactor, lineHeightFactor) tuned against this project's actual template and real
// generated content, plus countMathRows as a floor for matrix/aligned-equations content that
// renders taller than its character count alone would suggest. It converges toward a fitting scale
// over a handful of iterations, each re-measuring wrapped line count at the current candidate
// scale: both average character width and line height shrink together as font size shrinks, so the
// relationship between scale and space saved is closer to quadratic than linear (halving the font
// roughly quarters the space multi-line bullets need) — a single-pass linear guess undershoots how
// much scale actually helps. Sub-bullet (level >= 1) lines are counted the same as level-0 lines
// for this estimate even though they actually render in a smaller inherited font (see bulletLine)
// — a deliberately conservative simplification rather than a second layout lookup, since
// sub-bullets are a small minority of real content.
func estimateAutofitScale(bullets []bulletLine, geom bodyGeometry) *autofitScale {
	const (
		emuPerPoint        = 12700.0
		avgCharWidthFactor = 0.5  // average glyph width as a fraction of font size
		lineHeightFactor   = 1.2  // single-line-spacing factor for most sans-serif fonts
		minScale           = 0.25 // floor, matching PowerPoint's own effective autofit minimum
		maxIterations      = 20
	)
	widthPt := float64(geom.widthEMU) / emuPerPoint
	heightPt := float64(geom.heightEMU) / emuPerPoint
	fontSizePt := float64(geom.fontSizeHundredths) / 100
	lineSpaceFactor := float64(geom.lineSpacePermille) / 100000
	if widthPt <= 0 || heightPt <= 0 || fontSizePt <= 0 {
		return nil
	}

	charCounts := make([]int, len(bullets))
	mathRows := make([]int, len(bullets))
	for i, b := range bullets {
		charCounts[i] = len([]rune(b.text))
		mathRows[i] = countMathRows(b.text)
	}

	neededHeightAt := func(scale float64) float64 {
		fs := fontSizePt * scale
		charsPerLine := widthPt / (avgCharWidthFactor * fs)
		lineHeight := fs * lineHeightFactor * lineSpaceFactor
		var lines float64
		for i, n := range charCounts {
			l := math.Ceil(float64(n) / charsPerLine)
			if l < float64(mathRows[i]) {
				l = float64(mathRows[i])
			}
			lines += l
		}
		return lines * lineHeight
	}

	scale := 1.0
	for range maxIterations {
		needed := neededHeightAt(scale)
		if needed <= heightPt {
			break
		}
		scale *= math.Sqrt(heightPt / needed)
		if scale < minScale {
			scale = minScale
			break
		}
	}

	if scale >= 0.999 {
		return nil // fits at 100%; bare <a:normAutofit/> is enough
	}
	return &autofitScale{
		fontScale:      int(scale * 100000),
		lnSpcReduction: int((1 - scale) * 50000),
	}
}

// autofitScale holds an explicit, pre-computed text-autofit scale to bake into a shape's
// <a:normAutofit>, instead of the bare (percentage-less) form ensureNormAutofit otherwise inserts
// — see estimateAutofitScale for why a bare tag isn't always enough on its own. fontScale and
// lnSpcReduction are OOXML's own units: thousandths of a percent (e.g. 62000 means 62%).
type autofitScale struct {
	fontScale      int
	lnSpcReduction int
}

// normAutofitTag renders scale as a "<a:normAutofit .../>" element: bare if scale is nil, with
// explicit fontScale/lnSpcReduction attributes otherwise.
func normAutofitTag(scale *autofitScale) string {
	if scale == nil {
		return "<a:normAutofit/>"
	}
	return fmt.Sprintf(`<a:normAutofit fontScale="%d" lnSpcReduction="%d"/>`, scale.fontScale, scale.lnSpcReduction)
}

// ensureNormAutofit guarantees shape's <a:bodyPr> declares <a:normAutofit> ("shrink text on
// overflow", using scale's explicit percentages if non-nil — see estimateAutofitScale) when it
// has no autofit child at all, leaving a bodyPr that already declares one (normAutofit, noAutofit,
// or spAutoFit) untouched — that's a deliberate choice on this specific shape, not the gap this
// works around.
//
// PowerPoint's own placeholder inheritance for autofit is unreliable in practice: this package's
// template ships every placeholder's slide-level bodyPr empty ("<a:bodyPr/>", no autofit child at
// all) even though the corresponding slide layout declares "<a:normAutofit/>" — and PowerPoint
// does not reliably apply that inherited setting to generated slides until a user manually clicks
// Home > Reset on each one (observed against real generated decks: the layout's autofit is
// correctly configured, but slides opened fresh still overflow their placeholder box). Explicitly
// writing normAutofit onto every generated slide's own bodyPr, rather than relying on it being
// inherited from the layout, guarantees "shrink text on overflow" applies without that manual
// per-slide step.
func ensureNormAutofit(block []byte, scale *autofitScale) []byte {
	start := bytes.Index(block, []byte("<a:bodyPr"))
	if start == -1 {
		return block
	}
	tagEndRel := bytes.IndexByte(block[start:], '>')
	if tagEndRel == -1 {
		return block
	}
	tagEnd := start + tagEndRel // index of the opening tag's '>'
	tag := normAutofitTag(scale)

	if block[tagEnd-1] == '/' {
		// Self-closing "<a:bodyPr.../>": splice in an explicit close and normAutofit child.
		out := make([]byte, 0, len(block)+len(tag)+len("></a:bodyPr>"))
		out = append(out, block[:tagEnd-1]...)
		out = append(out, '>')
		out = append(out, tag...)
		out = append(out, []byte("</a:bodyPr>")...)
		out = append(out, block[tagEnd+1:]...)
		return out
	}

	closeRel := bytes.Index(block[tagEnd:], []byte("</a:bodyPr>"))
	if closeRel == -1 {
		return block // malformed/unclosed bodyPr; leave as-is rather than guess
	}
	content := block[tagEnd+1 : tagEnd+closeRel]

	// A normAutofit already present is replaced, not skipped, whenever scale is non-nil: the
	// existing one is routinely this exact shape's own tag from a previous call, carrying a stale
	// scale forward — duplicateContentSlides builds every slide by re-locating this same
	// placeholder shape inside bytes that already have the *previous* slide's rendered content
	// (including whatever autofit scale that slide computed for its own, different bullets), not
	// a pristine unmodified prototype each time. Treating a prior normAutofit as "already decided"
	// the way an author's genuine noAutofit/spAutoFit choice below is treated would silently pin
	// every slide after the first to that first slide's scale forever, regardless of how much
	// their own content actually needs — exactly the bug this replacement fixes. scale == nil
	// (the plain "ensure some autofit is set" caller) keeps the original skip-if-present behavior,
	// since it has no specific value of its own to enforce over whatever's already there.
	if loc := reNormAutofit.FindIndex(content); loc != nil {
		if scale == nil {
			return block
		}
		out := make([]byte, 0, len(block)+len(tag))
		out = append(out, block[:tagEnd+1+loc[0]]...)
		out = append(out, tag...)
		out = append(out, block[tagEnd+1+loc[1]:]...)
		return out
	}
	if reAutofitChild.Match(content) {
		return block // noAutofit or spAutoFit: a deliberate choice on this specific shape, not ours to override
	}

	insertAt := tagEnd + 1 // default: bodyPr's first child
	if warp := rePrstTxWarp.Find(content); warp != nil {
		insertAt += len(warp) // schema requires prstTxWarp, when present, before the autofit choice
	}

	out := make([]byte, 0, len(block)+len(tag))
	out = append(out, block[:insertAt]...)
	out = append(out, tag...)
	out = append(out, block[insertAt:]...)
	return out
}

// setPlaceholderBullets locates the <p:sp> shape containing a <p:ph type="phType" .../> and
// replaces its text body with one <a:p> paragraph per bullet, indented via <a:pPr lvl="1"/> for
// any bullet.level >= 1 (see bulletLine) — omitted for level 0, since 0 is OOXML's own implicit
// default level and doesn't need stating. scale, if non-nil, bakes an explicit autofit percentage
// into the shape's bodyPr (see estimateAutofitScale); nil gets the bare, percentage-less form
// ensureNormAutofit inserts by default.
func setPlaceholderBullets(slideXML []byte, phType string, bullets []bulletLine, scale *autofitScale, warnings *mathWarnings) ([]byte, error) {
	marker := []byte(`<p:ph type="` + phType + `"`)
	phIdx := bytes.Index(slideXML, marker)
	if phIdx == -1 {
		return nil, fmt.Errorf("no %q placeholder found", phType)
	}

	spStart := bytes.LastIndex(slideXML[:phIdx], []byte("<p:sp>"))
	if spStart == -1 {
		return nil, fmt.Errorf("no enclosing <p:sp> for %q placeholder", phType)
	}
	spEndRel := bytes.Index(slideXML[phIdx:], []byte("</p:sp>"))
	if spEndRel == -1 {
		return nil, fmt.Errorf("no closing </p:sp> for %q placeholder", phType)
	}
	spEnd := phIdx + spEndRel + len("</p:sp>")

	block := ensureNormAutofit(slideXML[spStart:spEnd], scale)

	lstIdx := bytes.Index(block, []byte("<a:lstStyle/>"))
	if lstIdx == -1 {
		return nil, fmt.Errorf("no <a:lstStyle/> in %q placeholder shape", phType)
	}
	insertPos := lstIdx + len("<a:lstStyle/>")
	closeRel := bytes.Index(block[insertPos:], []byte("</p:txBody>"))
	if closeRel == -1 {
		return nil, fmt.Errorf("no closing </p:txBody> in %q placeholder shape", phType)
	}
	closePos := insertPos + closeRel

	var paragraphs strings.Builder
	for _, b := range bullets {
		paragraphs.WriteString(`<a:p>`)
		if b.level >= 1 {
			paragraphs.WriteString(`<a:pPr lvl="1"/>`)
		}
		paragraphs.WriteString(runsXML(b.text, warnings))
		paragraphs.WriteString(`</a:p>`)
	}

	newBlock := make([]byte, 0, len(block)+paragraphs.Len())
	newBlock = append(newBlock, block[:insertPos]...)
	newBlock = append(newBlock, []byte(paragraphs.String())...)
	newBlock = append(newBlock, block[closePos:]...)

	out := make([]byte, 0, len(slideXML)-len(block)+len(newBlock))
	out = append(out, slideXML[:spStart]...)
	out = append(out, newBlock...)
	out = append(out, slideXML[spEnd:]...)
	return out, nil
}

// reMathSpan matches "\(...\)" inline math and "\[...\]" display math spans.
var reMathSpan = regexp.MustCompile(`\\\((.+?)\\\)|\\\[(.+?)\\\]`)

// reBold matches "**bold**" markdown spans.
var reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)

// reCodeSpan matches "`code`" inline markdown code spans.
var reCodeSpan = regexp.MustCompile("`(.+?)`")

// runsXML renders text as one or more <a:r>/math runs. Code spans are extracted first, ahead of
// bold-splitting and math extraction (see boldAwareRunsXML), so a code span's content is always
// rendered literally as a single monospace run — its markers ("**", "\(...\)") are never
// reinterpreted as bold or math. This matches CommonMark's precedence rule that code spans bind
// tighter than emphasis, and it's the right call for the actual use case: bullets use code spans
// for git commands, YAML keys, and template strings like "${{ secrets.MY_SECRET }}", none of
// which should have stray "**" or "\(...\)" inside them swept into markdown. The accepted
// tradeoff is that a bold-wrapped code span like "**`git status`**" doesn't render bold — the
// outer "**" markers end up as literal plain-text runs on either side of the code run, because
// bold-splitting only ever sees the text around the extracted span, not through it. That case
// hasn't shown up in practice (code spans mark up commands/config on their own, not emphasized
// prose), so it's left unhandled rather than adding another layer of precedence-juggling for it.
func runsXML(text string, warnings *mathWarnings) string {
	var b strings.Builder
	last := 0
	for _, loc := range reCodeSpan.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(boldAwareRunsXML(text[last:loc[0]], warnings))
		}
		b.WriteString(codeRunXML(text[loc[2]:loc[3]]))
		last = loc[1]
	}
	if last < len(text) {
		b.WriteString(boldAwareRunsXML(text[last:], warnings))
	}
	if b.Len() == 0 {
		b.WriteString(runXML(text, false))
	}
	return b.String()
}

// boldAwareRunsXML renders text (assumed free of code-span backticks — runsXML extracts those
// before calling this) as one or more <a:r>/math runs. Bold spans are split first, before math
// extraction runs on each resulting segment — the reverse of this package's original order.
// LLM output routinely bolds a phrase that also contains inline math ("**\(n\)-tuple**", not just
// a formula bolded on its own), and extracting math spans across the whole string before bold-
// splitting broke that case: whenever a bold span's opening and closing "**" landed on either side
// of an embedded math span (as they do in "**\(n\)-tuple**" — the math span "\(n\)" sits between
// them), the runs of plain text before and after the math span were rendered by two separate calls
// that each saw only one of the two "**" markers, so neither ever matched as a complete bold span
// — both leaked through as literal asterisks, and the plain-text portion of the bold phrase
// ("-tuple") rendered unbolded. Splitting on bold first means each segment's math spans are found
// within text that's already known to be entirely bold or entirely not, so a bold segment's own
// plain-text portions render bold correctly regardless of where a math span inside it falls.
// Math itself is still never rendered bold, even inside a bold segment — an accepted tradeoff
// (forcing bold onto pandoc-generated OMML output isn't worth the complexity for how rarely a
// bolded formula appears at all, and this matches the pre-existing behavior for a formula bolded
// on its own).
func boldAwareRunsXML(text string, warnings *mathWarnings) string {
	var b strings.Builder
	last := 0
	for _, loc := range reBold.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(mathAwareRunsXML(text[last:loc[0]], false, warnings))
		}
		b.WriteString(mathAwareRunsXML(text[loc[2]:loc[3]], true, warnings))
		last = loc[1]
	}
	if last < len(text) {
		b.WriteString(mathAwareRunsXML(text[last:], false, warnings))
	}
	return b.String()
}

// mathAwareRunsXML renders text (assumed free of "**bold**" markers — runsXML strips those before
// calling this) as one or more <a:r>/math runs, rendering "\(...\)"/"\[...\]" spans as math (never
// bold, see runsXML) and everything else as plain text runs bolded per bold.
func mathAwareRunsXML(text string, bold bool, warnings *mathWarnings) string {
	var b strings.Builder
	last := 0
	for _, loc := range reMathSpan.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(runXML(text[last:loc[0]], bold))
		}
		switch {
		case loc[2] != -1: // \(inline math\)
			b.WriteString(mathRunXML(text[loc[2]:loc[3]], [2]string{`\(`, `\)`}, warnings))
		case loc[4] != -1: // \[display math\]
			b.WriteString(mathRunXML(text[loc[4]:loc[5]], [2]string{`\[`, `\]`}, warnings))
		}
		last = loc[1]
	}
	if last < len(text) {
		b.WriteString(runXML(text[last:], bold))
	}
	return b.String()
}

// runXML renders a single <a:r> run, bold if requested.
func runXML(text string, bold bool) string {
	rPr := `<a:rPr lang="en-US" dirty="0"/>`
	if bold {
		rPr = `<a:rPr lang="en-US" b="1" dirty="0"/>`
	}
	return `<a:r>` + rPr + `<a:t>` + xmlTextReplacer.Replace(text) + `</a:t></a:r>`
}

// codeRunXML renders a single <a:r> run for an inline "`code`" span, in the Consolas monospace
// font. Content goes through the same xmlTextReplacer escaping as any other run — a code span's
// "<", ">", and "&" are exactly as unsafe in <a:t> as they'd be in plain text, so this reuses
// runXML's escaping path rather than reimplementing it.
func codeRunXML(text string) string {
	return `<a:r><a:rPr lang="en-US" dirty="0"><a:latin typeface="Consolas"/></a:rPr><a:t>` + xmlTextReplacer.Replace(text) + `</a:t></a:r>`
}

// bulletLine is one bullet's text and indentation level (0 = top-level, 1 = an indented
// sub-bullet), matching distill.Slide.Content's marker-free convention: each line is implicitly
// its own bullet, with a leading two-space indent (preserved by distill.bulletLines) the only
// signal a line is a sub-bullet rather than a top-level one.
type bulletLine struct {
	text  string
	level int
}

// splitBullets splits content on newlines into non-empty bullet lines, detecting each line's
// level from a leading two-space indent (see bulletLine) before trimming the rest of the
// whitespace off.
func splitBullets(content string) []bulletLine {
	lines := strings.Split(content, "\n")
	bullets := make([]bulletLine, 0, len(lines))
	for _, l := range lines {
		trimmedRight := strings.TrimRight(l, " \t\r") // \r for CRLF content: strings.Split on "\n" alone leaves it dangling
		trimmed := strings.TrimLeft(trimmedRight, " \t")
		if trimmed == "" {
			continue
		}
		level := 0
		if indent := len(trimmedRight) - len(trimmed); indent >= 2 {
			level = 1
		}
		bullets = append(bullets, bulletLine{text: trimmed, level: level})
	}
	return bullets
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
