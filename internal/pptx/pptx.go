// Package pptx provides PPTX rendering from distilled context and text templates.
package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
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
func Render(templatePath string, dc *distill.DistilledContext, courseName string, vars map[string]string, outputPath string) error {
	inData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read pptx template %q: %w", templatePath, err)
	}

	reader, err := zip.NewReader(bytes.NewReader(inData), int64(len(inData)))
	if err != nil {
		return fmt.Errorf("open pptx template %q: %w", templatePath, err)
	}

	parts, headers, order, err := readParts(reader)
	if err != nil {
		return err
	}

	markLayoutPicturesDecorative(parts)
	resetLastView(parts)

	if err := applyDeck(parts, &order, dc, courseName); err != nil {
		return err
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
			return err
		}
	}

	closeWriter = false
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finalize output pptx: %w", err)
	}

	return nil
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
func applyDeck(parts map[string][]byte, order *[]string, dc *distill.DistilledContext, courseName string) error {
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

	if err := fillTitleSlide(parts, titleSlide, dc.ModuleName, courseName); err != nil {
		return err
	}

	if err := fillAgenda(parts, agendaSlide, dc.Agenda); err != nil {
		return err
	}

	contentSldIDs, err := duplicateContentSlides(parts, order, contentSlide, dc.Slides)
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
func fillTitleSlide(parts map[string][]byte, slidePart, title, courseName string) error {
	updated, err := setPlaceholderBullets(parts[slidePart], "title", []string{title})
	if err != nil {
		return fmt.Errorf("fill title slide %q: %w", slidePart, err)
	}
	if courseName != "" {
		updated, err = setPlaceholderBullets(updated, "body", []string{courseName})
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
		relsData, ok := parts[relsPartFor(name)]
		if !ok {
			continue
		}
		rm := reSlideLayoutRel.FindSubmatch(relsData)
		if rm == nil {
			continue
		}
		layoutPart := resolveRelTarget(path.Dir(name), string(rm[1]))
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

func fillAgenda(parts map[string][]byte, slidePart string, agenda []string) error {
	if n := len(agenda); n < 3 || n > 8 {
		return fmt.Errorf("agenda must have between 3 and 8 bullets, got %d", n)
	}
	updated, err := setPlaceholderBullets(parts[slidePart], "body", agenda)
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
func duplicateContentSlides(parts map[string][]byte, order *[]string, prototypePart string, slides []distill.Slide) ([]string, error) {
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

	sldIDs := make([]string, len(slides))

	for i, slide := range slides {
		body, err := setPlaceholderBullets(parts[prototypePart], "title", []string{slide.Title})
		if err != nil {
			return nil, fmt.Errorf("set title for slide %d: %w", i+1, err)
		}
		body, err = setPlaceholderBullets(body, "body", splitBullets(slide.Content))
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

// rePrstTxWarp matches a complete <a:prstTxWarp> element, self-closing or with children (e.g. an
// <a:avLst> of adjustment values) — CT_TextBodyProperties's schema requires prstTxWarp, when
// present, to precede the autofit choice, so ensureNormAutofit must insert normAutofit after it
// rather than as bodyPr's first child.
var rePrstTxWarp = regexp.MustCompile(`(?s)^<a:prstTxWarp\b(?:[^>]*/>|[^>]*>.*?</a:prstTxWarp>)`)

// ensureNormAutofit guarantees shape's <a:bodyPr> declares <a:normAutofit/> ("shrink text on
// overflow") when it has no autofit child at all, leaving a bodyPr that already declares one
// (normAutofit, noAutofit, or spAutoFit) untouched — that's a deliberate choice on this specific
// shape, not the gap this works around.
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
func ensureNormAutofit(block []byte) []byte {
	start := bytes.Index(block, []byte("<a:bodyPr"))
	if start == -1 {
		return block
	}
	tagEndRel := bytes.IndexByte(block[start:], '>')
	if tagEndRel == -1 {
		return block
	}
	tagEnd := start + tagEndRel // index of the opening tag's '>'

	if block[tagEnd-1] == '/' {
		// Self-closing "<a:bodyPr.../>": splice in an explicit close and normAutofit child.
		out := make([]byte, 0, len(block)+len("><a:normAutofit/></a:bodyPr>"))
		out = append(out, block[:tagEnd-1]...)
		out = append(out, []byte("><a:normAutofit/></a:bodyPr>")...)
		out = append(out, block[tagEnd+1:]...)
		return out
	}

	closeRel := bytes.Index(block[tagEnd:], []byte("</a:bodyPr>"))
	if closeRel == -1 {
		return block // malformed/unclosed bodyPr; leave as-is rather than guess
	}
	content := block[tagEnd+1 : tagEnd+closeRel]
	if reAutofitChild.Match(content) {
		return block // already has an explicit autofit choice
	}

	insertAt := tagEnd + 1 // default: bodyPr's first child
	if warp := rePrstTxWarp.Find(content); warp != nil {
		insertAt += len(warp) // schema requires prstTxWarp, when present, before the autofit choice
	}

	out := make([]byte, 0, len(block)+len("<a:normAutofit/>"))
	out = append(out, block[:insertAt]...)
	out = append(out, []byte("<a:normAutofit/>")...)
	out = append(out, block[insertAt:]...)
	return out
}

// setPlaceholderBullets locates the <p:sp> shape containing a <p:ph type="phType" .../> and
// replaces its text body with one <a:p> paragraph per bullet.
func setPlaceholderBullets(slideXML []byte, phType string, bullets []string) ([]byte, error) {
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

	block := ensureNormAutofit(slideXML[spStart:spEnd])

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
		paragraphs.WriteString(runsXML(b))
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
func runsXML(text string) string {
	var b strings.Builder
	last := 0
	for _, loc := range reBold.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(mathAwareRunsXML(text[last:loc[0]], false))
		}
		b.WriteString(mathAwareRunsXML(text[loc[2]:loc[3]], true))
		last = loc[1]
	}
	if last < len(text) {
		b.WriteString(mathAwareRunsXML(text[last:], false))
	}
	if b.Len() == 0 {
		b.WriteString(runXML(text, false))
	}
	return b.String()
}

// mathAwareRunsXML renders text (assumed free of "**bold**" markers — runsXML strips those before
// calling this) as one or more <a:r>/math runs, rendering "\(...\)"/"\[...\]" spans as math (never
// bold, see runsXML) and everything else as plain text runs bolded per bold.
func mathAwareRunsXML(text string, bold bool) string {
	var b strings.Builder
	last := 0
	for _, loc := range reMathSpan.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > last {
			b.WriteString(runXML(text[last:loc[0]], bold))
		}
		switch {
		case loc[2] != -1: // \(inline math\)
			b.WriteString(mathRunXML(text[loc[2]:loc[3]], [2]string{`\(`, `\)`}))
		case loc[4] != -1: // \[display math\]
			b.WriteString(mathRunXML(text[loc[4]:loc[5]], [2]string{`\[`, `\]`}))
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

// splitBullets splits content on newlines into trimmed, non-empty bullet lines.
func splitBullets(content string) []string {
	lines := strings.Split(content, "\n")
	bullets := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		bullets = append(bullets, l)
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
