package extract

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jh125486/pdf2qti/internal/config"
)

var markdownHeading = regexp.MustCompile(`^ {0,3}##(?:[\t ]+(.*))?$`)
var markdownTopHeading = regexp.MustCompile(`^#(?:[\t ]|$)`)
var markdownClosingHashes = regexp.MustCompile(`[\t ]+#+[\t ]*$`)
var markdownListMarker = regexp.MustCompile(`^(?:[-+*]|\d{1,9}[.)]) +`)
var markdownSectionID = regexp.MustCompile(`^Section[\t ]+(\d+(?:\.\d+)*)(?:[\t ]|$)`)

type markdownSection struct {
	title string
	id    string
	start int
	end   int
}

// ReadMarkdown reads configured Markdown files in order. Each level-two heading
// starts a section, ending at the next level-one or level-two heading. Nested
// headings and fenced code remain intact. Selectors match heading text or the
// numeric ID in a heading such as "Section 1.2 - Introduction". Matching sections
// retain document order; preamble is included only when explicitly requested.
func ReadMarkdown(files []config.ContextFile) (string, error) {
	var parts []string
	for _, file := range files {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return "", fmt.Errorf("read Markdown %q: %w", file.Path, err)
		}
		text, err := selectMarkdown(string(data), file)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}
	text := strings.Join(parts, "\n\n")
	if text == "" {
		return "", fmt.Errorf("markdown context contains no selected text")
	}
	return text, nil
}

func selectMarkdown(text string, file config.ContextFile) (string, error) {
	sections, preambleEnd := markdownSections(text)
	selected := make(map[int]bool)
	for _, selector := range file.Sections {
		found := false
		for i, section := range sections {
			if selector == section.title || (section.id != "" && selector == section.id) {
				selected[i] = true
				found = true
			}
		}
		if !found {
			available := make([]string, len(sections))
			for i, section := range sections {
				available[i] = section.title
			}
			return "", fmt.Errorf("markdown %q: unknown section %q; available sections: %v", file.Path, selector, available)
		}
	}
	var parts []string
	if file.IncludePreamble {
		parts = append(parts, text[:preambleEnd])
	}
	for i, section := range sections {
		if len(file.Sections) == 0 || selected[i] {
			parts = append(parts, text[section.start:section.end])
		}
	}
	return strings.Join(parts, "\n"), nil
}

func markdownSections(text string) (sections []markdownSection, preambleEnd int) {
	preambleEnd = len(text)
	offset := 0
	var fence markdownFence
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), " ")
		indent := len(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")) - len(trimmed)
		if !fence.consume(trimmed, indent) && fence.marker == 0 && indent <= 3 {
			heading := markdownHeading.FindStringSubmatch(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
			topLevel := markdownTopHeading.MatchString(trimmed)
			if len(heading) > 0 || topLevel {
				if len(sections) > 0 && sections[len(sections)-1].end == len(text) {
					sections[len(sections)-1].end = offset
				}
				if len(heading) > 0 {
					if len(sections) == 0 {
						preambleEnd = offset
					}
					sections = append(sections, newMarkdownSection(heading[1], offset, len(text)))
				}
			}
		}
		offset += len(line)
	}
	return sections, preambleEnd
}

// markdownFence retains the list content indent so a closing fence may be more
// than three columns from the document margin, while still within three columns
// of its container. List markers inside an active code fence remain literal text.
type markdownFence struct {
	marker          byte
	length          int
	containerIndent int
}

func (f *markdownFence) consume(line string, indent int) bool {
	// Leaving the list also ends its fenced code block. Blank lines do not end
	// the container; a dedented nonblank line must be reconsidered as Markdown.
	if f.containerIndent > 0 && indent < f.containerIndent && strings.TrimSpace(line) != "" {
		*f = markdownFence{}
	}
	if f.marker != 0 {
		if indent < f.containerIndent || indent > f.containerIndent+3 || !isFenceLine(line) {
			return false
		}
		f.marker, f.length = updateFence(line, f.marker, f.length)
		return true
	}
	if indent > 3 {
		return false
	}
	candidate, listIndent := stripMarkdownListMarkers(line)
	if !isFenceLine(candidate) {
		return false
	}
	f.marker, f.length = updateFence(candidate, 0, 0)
	f.containerIndent = 0
	if listIndent > 0 {
		f.containerIndent = indent + listIndent
	}
	return true
}

func stripMarkdownListMarkers(line string) (stripped string, width int) {
	stripped = line
	for {
		marker := markdownListMarker.FindString(stripped)
		if marker == "" {
			return stripped, width
		}
		width += len(marker)
		stripped = stripped[len(marker):]
	}
}

func isFenceLine(line string) bool {
	return line != "" && (line[0] == '`' || line[0] == '~')
}

func updateFence(line string, fence byte, fenceLength int) (marker byte, length int) {
	marker = line[0]
	for length < len(line) && line[length] == marker {
		length++
	}
	if fence == 0 && length >= 3 && (marker != '`' || !strings.ContainsRune(line[length:], '`')) {
		return marker, length
	}
	if fence == marker && length >= fenceLength && strings.TrimSpace(line[length:]) == "" {
		return 0, 0
	}
	return fence, fenceLength
}

func newMarkdownSection(heading string, start, end int) markdownSection {
	section := markdownSection{title: strings.TrimSpace(markdownClosingHashes.ReplaceAllString(heading, "")), start: start, end: end}
	if id := markdownSectionID.FindStringSubmatch(section.title); len(id) > 0 {
		section.id = id[1]
	}
	return section
}
