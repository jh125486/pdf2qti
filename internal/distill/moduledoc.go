package distill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
)

// TaggedSection is a chapter's Section attributed to the chapter it came from, so a module
// doc can group sections (and later, slides) by chapter.
type TaggedSection struct {
	Title       string
	Summary     string
	ChapterTag  string // matches the chapter's config.Source.ID / ProtoChapterInput.Tag
	ChapterName string
}

// ModuleDoc is the combined, self-contained document for a module spanning one or more
// distilled chapters: a synthesized overview, merged learning objectives/vocabulary/theorems,
// each chapter's sections (tagged by chapter), and a slide deck in proto-deck markdown format.
type ModuleDoc struct {
	ModuleID   string
	ModuleName string
	Overview   string
	Objectives []Objective
	Vocabulary []VocabTerm
	Theorems   []Theorem
	Sections   []TaggedSection
	SlidesMD   string // raw GenerateProtoDeck output: headers, meta markers, and --- separators included
}

// BuildModuleDoc synthesizes chapters (already-distilled) into a single ModuleDoc: one LLM
// call merges overview/objectives/vocabulary/theorems across all chapters, GenerateProtoDeck
// produces the tagged slide deck, and each chapter's own Sections are flattened deterministically
// (not synthesized, so the chapter grouping they carry isn't lost).
func BuildModuleDoc(ctx context.Context, llm LLM, moduleID, moduleName string, chapters []*DistilledContext, minSlides, maxSlides int) (*ModuleDoc, error) {
	if len(chapters) == 0 {
		return nil, errors.New("no chapters to build a module doc from")
	}

	prompt, err := buildModuleDocPrompt(chapters)
	if err != nil {
		return nil, fmt.Errorf("build module doc prompt: %w", err)
	}
	raw, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm complete: %w", err)
	}
	var resp moduleDocResponse
	if err := unmarshalRepaired(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	protoChapters := make([]ProtoChapterInput, len(chapters))
	for i, c := range chapters {
		protoChapters[i] = ProtoChapterInput{
			Tag:           c.SourceID,
			ModuleName:    c.ModuleName,
			Overview:      c.Overview,
			KeyConcepts:   c.KeyConcepts,
			TeachingNotes: c.TeachingNotes,
			Text:          c.Text,
		}
	}
	slidesMD, err := GenerateProtoDeck(ctx, llm, protoChapters, minSlides, maxSlides)
	if err != nil {
		return nil, fmt.Errorf("generate proto deck: %w", err)
	}

	var sections []TaggedSection
	for _, c := range chapters {
		name := c.ModuleName
		if name == "" {
			name = c.Book
		}
		for _, s := range c.Sections {
			sections = append(sections, TaggedSection{
				Title:       s.Title,
				Summary:     s.Summary,
				ChapterTag:  c.SourceID,
				ChapterName: name,
			})
		}
	}

	return &ModuleDoc{
		ModuleID:   moduleID,
		ModuleName: moduleName,
		Overview:   resp.Overview,
		Objectives: resp.Objectives,
		Vocabulary: resp.Vocabulary,
		Theorems:   resp.Theorems,
		Sections:   sections,
		SlidesMD:   slidesMD,
	}, nil
}

// moduleDocResponse is the shape of the LLM's JSON response to moduleDocPromptTmpl.
type moduleDocResponse struct {
	Overview   string      `json:"overview"`
	Objectives []Objective `json:"objectives"`
	Vocabulary vocabList   `json:"vocabulary"`
	Theorems   []Theorem   `json:"theorems"`
}

// moduleDocPromptTmpl is the LLM prompt template for cross-chapter module synthesis.
var moduleDocPromptTmpl = template.Must(template.New("moduledoc").Parse(`You are synthesizing several distilled textbook chapters into ONE combined module-level summary.

## Chapters
{{range .Chapters}}### {{.Book}} (chapter {{.Chapter}})
Overview: {{.Overview}}
Key concepts: {{range .KeyConcepts}}{{.}}, {{end}}
Objectives: {{range .Objectives}}CO{{.CO}}: {{.Text}}; {{end}}
Vocabulary: {{range .Vocabulary}}{{.Term}}: {{.Definition}}; {{end}}
Theorems: {{range .Theorems}}{{.Name}}: {{.Statement}}; {{end}}

{{end}}
## Task
Produce a JSON object with these exact fields, synthesizing across ALL chapters above into one
cohesive module rather than separate per-chapter blurbs stitched together:
- overview: 2-4 sentences of cohesive prose introducing the whole module as a single unit
- objectives: array of {co, text}, merging every chapter's objectives and removing exact duplicates
- vocabulary: array of {term, definition}, merging every chapter's vocabulary and removing
  duplicate terms (case-insensitive); prefer the clearest definition when terms repeat
- theorems: array of {name, statement}, merging every chapter's theorems and removing duplicate
  names; return an empty array if no chapter has any
`))

// moduleDocPromptData is the data passed to moduleDocPromptTmpl.
type moduleDocPromptData struct {
	Chapters []*DistilledContext
}

// buildModuleDocPrompt renders the LLM prompt for cross-chapter module synthesis.
func buildModuleDocPrompt(chapters []*DistilledContext) (string, error) {
	var buf bytes.Buffer
	if err := moduleDocPromptTmpl.Execute(&buf, moduleDocPromptData{Chapters: chapters}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderModuleMarkdown renders doc as a self-contained Markdown document: Overview, Learning
// Objectives, Vocabulary, Useful Theorems (omitted entirely if empty), Sections (grouped by
// chapter), and Slides (the embedded proto-deck markdown, verbatim).
func RenderModuleMarkdown(doc *ModuleDoc) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", doc.ModuleName)

	b.WriteString("## Overview\n\n")
	b.WriteString(doc.Overview)
	b.WriteString("\n\n")

	b.WriteString("## Learning Objectives\n\n")
	for _, o := range doc.Objectives {
		fmt.Fprintf(&b, "- CO%d: %s\n", o.CO, o.Text)
	}
	b.WriteString("\n")

	b.WriteString("## Vocabulary\n\n")
	for _, v := range doc.Vocabulary {
		fmt.Fprintf(&b, "- **%s** — %s\n", v.Term, v.Definition)
	}
	b.WriteString("\n")

	if len(doc.Theorems) > 0 {
		b.WriteString("## Useful Theorems\n\n")
		for _, t := range doc.Theorems {
			fmt.Fprintf(&b, "- **%s**: %s\n", t.Name, t.Statement)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Sections\n\n")
	lastTag := ""
	for _, s := range doc.Sections {
		if s.ChapterTag != lastTag {
			fmt.Fprintf(&b, "### %s\n\n", s.ChapterName)
			lastTag = s.ChapterTag
		}
		fmt.Fprintf(&b, "#### %s\n\n%s\n\n", s.Title, s.Summary)
	}

	b.WriteString("## Slides\n\n")
	b.WriteString(doc.SlidesMD)
	b.WriteString("\n")

	return b.String()
}

// SaveModuleDoc writes doc as JSON to path, creating or truncating the file.
func SaveModuleDoc(path string, doc *ModuleDoc) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal module doc: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write module doc %q: %w", path, err)
	}
	return nil
}

// LoadModuleDoc reads and parses a ModuleDoc from a JSON file at path.
func LoadModuleDoc(path string) (*ModuleDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read module doc %q: %w", path, err)
	}
	var doc ModuleDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse module doc %q: %w", path, err)
	}
	return &doc, nil
}

// SaveModuleMarkdown writes md to path, creating or truncating the file.
func SaveModuleMarkdown(path, md string) error {
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		return fmt.Errorf("write module markdown %q: %w", path, err)
	}
	return nil
}
