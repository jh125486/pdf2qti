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

// Render reads a PPTX template file, validates it has Title/Agenda/Content slide layouts, fills
// in the title slide (dc.ModuleName and courseName), the agenda bullets, and duplicates the
// Content slide once per dc.Slides entry, executes Go text templates in the remaining XML/RELS
// parts against distilled context data and vars, and writes the result to outputPath.
//
// warnings reports every formula that failed to convert to real OOXML math and fell back to
// plain escaped text instead — see mathWarnings — so callers can surface them instead of a
// broken formula silently shipping unrendered (nil, not an error, on full success).
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
	if err := applyDeck(parts, &order, dc, courseName, mathW); err != nil {
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

	return mathW.warnings(), nil
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

// applyDeck validates the template's slide layouts and mutates parts/order in place to fill the
// title slide, inject agenda bullets into the Agenda slide, and duplicate the Content slide once
// per dc.Slides entry.
func applyDeck(parts map[string][]byte, order *[]string, dc *distill.DistilledContext, courseName string, warnings *mathWarnings) error {
	layoutNames, err := layoutNamesByPart(parts)
	if err != nil {
		return err
	}
	if err := validateLayouts(layoutNames); err != nil {
		return err
	}

	slidesByLayout := slidesByLayoutName(parts, *order, layoutNames)

	titleSlide, ok := slidesByLayout[layoutTitle]
	if !ok {
		return fmt.Errorf("template has no slide using the %q layout", layoutTitle)
	}
	agendaSlide, ok := slidesByLayout[layoutAgenda]
	if !ok {
		return fmt.Errorf("template has no slide using the %q layout", layoutAgenda)
	}
	contentSlide, ok := slidesByLayout[layoutContent]
	if !ok {
		return fmt.Errorf("template has no slide using the %q layout", layoutContent)
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

	if err := fillTitleSlide(parts, titleSlide, dc.ModuleName, courseName, warnings); err != nil {
		return err
	}

	if err := fillAgenda(parts, agendaSlide, dc.Agenda, warnings); err != nil {
		return err
	}

	contentSldIDs, err := duplicateContentSlides(parts, order, contentSlide, dc.Slides, warnings)
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

func validateLayouts(layoutNames map[string]string) error {
	have := make(map[string]bool, len(layoutNames))
	for _, n := range layoutNames {
		have[n] = true
	}
	var missing []string
	for _, want := range []string{layoutTitle, layoutAgenda, layoutContent} {
		if !have[want] {
			missing = append(missing, want)
		}
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

// duplicateContentSlides fills the prototype Content slide with slides[0] in place, then clones
// it once per remaining entry, wiring up the new part's relationships, content type, and
// presentation slide list entry. It returns each entry's final numeric sldId, in slides order,
// so callers can group them into PowerPoint Sections afterward.
func duplicateContentSlides(parts map[string][]byte, order *[]string, prototypePart string, slides []distill.Slide, warnings *mathWarnings) ([]string, error) {
	if len(slides) == 0 {
		return nil, errors.New("distilled context has no slides to render")
	}

	prototypeRels, ok := parts[relsPartFor(prototypePart)]
	if !ok {
		return nil, fmt.Errorf("content slide %q has no relationships part", prototypePart)
	}

	const presRelsPart = "ppt/_rels/presentation.xml.rels"
	prototypeTarget := strings.TrimPrefix(prototypePart, "ppt/")
	prevRID, err := relationshipIDForTarget(parts[presRelsPart], prototypeTarget)
	if err != nil {
		return nil, fmt.Errorf("find presentation relationship for %q: %w", prototypePart, err)
	}

	presData := parts["ppt/presentation.xml"]
	protoSldID, err := sldIDForRID(presData, prevRID)
	if err != nil {
		return nil, err
	}

	nextSlideNum := maxSlideNumber(*order) + 1
	nextRID := maxRelID(parts[presRelsPart]) + 1
	nextSldID := maxSldID(presData) + 1

	// Resolved once, from the prototype's own layout, since every duplicated slide shares it.
	// geomOK false (no layout, or the layout's body placeholder doesn't match the expected shape)
	// just means every slide falls back to setPlaceholderBullets's bare-normAutofit default.
	var geom bodyGeometry
	var geomOK bool
	if layoutPart, ok := slideLayoutPart(parts, prototypePart); ok {
		geom, geomOK = contentBodyGeometry(parts[layoutPart])
	}

	sldIDs := make([]string, len(slides))

	for i, slide := range slides {
		body, err := setPlaceholderBullets(parts[prototypePart], "title", []bulletLine{{text: slide.Title}}, nil, warnings)
		if err != nil {
			return nil, fmt.Errorf("set title for slide %d: %w", i+1, err)
		}
		bullets := splitBullets(slide.Content)
		var scale *autofitScale
		if geomOK {
			scale = estimateAutofitScale(bullets, geom)
		}
		body, err = setPlaceholderBullets(body, "body", bullets, scale, warnings)
		if err != nil {
			return nil, fmt.Errorf("set content for slide %d: %w", i+1, err)
		}

		if i == 0 {
			parts[prototypePart] = body
			sldIDs[0] = protoSldID
			continue
		}

		slidePartName := fmt.Sprintf("ppt/slides/slide%d.xml", nextSlideNum)
		relsPartName := relsPartFor(slidePartName)
		nextSlideNum++

		parts[slidePartName] = body
		parts[relsPartName] = prototypeRels
		*order = append(*order, slidePartName, relsPartName)

		rID := fmt.Sprintf("rId%d", nextRID)
		nextRID++

		parts["[Content_Types].xml"], err = addContentTypeOverride(parts["[Content_Types].xml"], slidePartName)
		if err != nil {
			return nil, err
		}
		parts[presRelsPart], err = addPresentationRelationship(parts[presRelsPart], rID, strings.TrimPrefix(slidePartName, "ppt/"))
		if err != nil {
			return nil, err
		}

		presData = insertSldIDAfter(presData, prevRID, strconv.Itoa(nextSldID), rID)
		sldIDs[i] = strconv.Itoa(nextSldID)
		prevRID = rID
		nextSldID++
	}

	parts["ppt/presentation.xml"] = presData
	return sldIDs, nil
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

	marker := []byte(fmt.Sprintf(`r:id=%q/>`, afterRID))
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

// runsXML renders text as one or more <a:r>/math runs. Bold spans are split first, before math
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
func runsXML(text string, warnings *mathWarnings) string {
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
	if b.Len() == 0 {
		b.WriteString(runXML(text, false))
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
