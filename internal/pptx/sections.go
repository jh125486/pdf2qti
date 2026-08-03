package pptx

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jh125486/pdf2qti/internal/distill"
)

// sectionGroup is one PowerPoint Section: a display name and the numeric sldIds it spans.
type sectionGroup struct {
	name   string
	sldIDs []string
}

// addSections groups the deck's slides into PowerPoint Sections — "Introduction" (title +
// agenda), one section per distinct chapter tag in slides (in order), and "Summary" for any
// slide tagged "summary" — and inserts the resulting <p14:sectionLst> extension into presData.
func addSections(presData []byte, titleSldID, agendaSldID string, slides []distill.Slide, contentSldIDs []string) []byte {
	groups := buildSectionGroups(titleSldID, agendaSldID, slides, contentSldIDs)
	return insertSectionExt(presData, renderSectionListXML(groups))
}

// buildSectionGroups groups consecutive slides sharing the same Tag into one section, in
// document order, after a leading "Introduction" section for the title and agenda slides.
func buildSectionGroups(titleSldID, agendaSldID string, slides []distill.Slide, contentSldIDs []string) []sectionGroup {
	groups := []sectionGroup{{name: "Introduction", sldIDs: []string{titleSldID, agendaSldID}}}
	for i, slide := range slides {
		name := slide.Tag
		if strings.EqualFold(name, "summary") {
			name = "Summary"
		}
		if last := &groups[len(groups)-1]; last.name == name {
			last.sldIDs = append(last.sldIDs, contentSldIDs[i])
			continue
		}
		groups = append(groups, sectionGroup{name: name, sldIDs: []string{contentSldIDs[i]}})
	}
	return groups
}

// renderSectionListXML renders groups as a <p:ext> element containing a <p14:sectionLst>, ready
// to insert into ppt/presentation.xml's <p:extLst>.
func renderSectionListXML(groups []sectionGroup) string {
	var b strings.Builder
	b.WriteString(`<p:ext uri="{521415D9-36F7-43E2-AB2F-B90AF26B5E84}">`)
	b.WriteString(`<p14:sectionLst xmlns:p14="http://schemas.microsoft.com/office/powerpoint/2010/main">`)
	for _, g := range groups {
		fmt.Fprintf(&b, `<p14:section name=%q id=%q>`, xmlTextReplacer.Replace(g.name), newSectionID())
		b.WriteString(`<p14:sldIdLst>`)
		for _, id := range g.sldIDs {
			fmt.Fprintf(&b, `<p14:sldId id=%q/>`, id)
		}
		b.WriteString(`</p14:sldIdLst></p14:section>`)
	}
	b.WriteString(`</p14:sectionLst></p:ext>`)
	return b.String()
}

// insertSectionExt inserts extXML into presData's existing <p:extLst> if it has one, or wraps it
// in a new <p:extLst> just before </p:presentation> otherwise.
func insertSectionExt(presData []byte, extXML string) []byte {
	if idx := bytes.Index(presData, []byte("</p:extLst>")); idx != -1 {
		out := make([]byte, 0, len(presData)+len(extXML))
		out = append(out, presData[:idx]...)
		out = append(out, extXML...)
		out = append(out, presData[idx:]...)
		return out
	}
	wrapped := "<p:extLst>" + extXML + "</p:extLst>"
	return bytes.Replace(presData, []byte("</p:presentation>"), []byte(wrapped+"</p:presentation>"), 1)
}

// sectionIDFallbackSeq is only touched when crypto/rand.Read fails, to keep fallback IDs unique
// within a process even if called multiple times in the same nanosecond.
var sectionIDFallbackSeq atomic.Uint32

// newSectionID generates a GUID-formatted id, as PowerPoint's Section schema requires. If the
// system CSPRNG is unavailable, it falls back to a timestamp+counter-derived id instead of
// silently emitting an all-zero GUID (which would collide across every section in the deck).
func newSectionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[0:8], uint64(time.Now().UnixNano())) //nolint:gosec // fallback path only; not used for security
		binary.BigEndian.PutUint32(b[8:12], sectionIDFallbackSeq.Add(1))
	}
	return fmt.Sprintf("{%X-%X-%X-%X-%X}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
