package distill

import (
	"bytes"
	"text/template"

	"github.com/jh125486/pdf2qti/internal/config"
)

// distillPromptTmpl is the LLM prompt template for distillation.
var distillPromptTmpl = template.Must(template.New("distill").Parse(`You are distilling a textbook chapter for a systems programming course.

## Course Objectives
{{range .CourseObjectives}}{{.CO}}. {{.Text}}
{{end}}
## Task
Given the chapter text below, produce a JSON object with these exact fields:
- module_name: short title (e.g. "Signals")
- text: thick distilled prose (see requirements below)
- overview: 2-3 HTML <p> paragraphs suitable for a Canvas module overview, using "we" voice
- key_concepts: array of important technical terms, function names, constants, and data structures
- material_overview: 1-2 plain-text sentences describing what the chapter covers and why
- teaching_notes: slide-writing guidance (see requirements below)
- objectives: array of {co, text} for each course objective that this chapter satisfies

## text requirements
Write enough that a professor who has never read the chapter could write both 20 quiz questions
AND a 20-slide PowerPoint deck from this field alone. Include all of:
- Every testable fact: API names, constants, flags, error codes, return values
- Code patterns with rationale (why the pattern exists, not just what it is)
- "Why it works this way" historical or design context
- Edge cases and failure modes with their errno values
- Common misconceptions and the correct model
- Conceptual relationships between topics (e.g. how fork() affects file descriptor sharing)
- Standards vs. Linux-specific tradeoffs

## teaching_notes requirements
Slide-writing guidance distinct from the content itself. How to *teach* this chapter:
- Recommended slide structure and narrative arc
- Which details belong in a later chapter and should be deferred
- What to emphasize as "why" vs. "what"
- Concepts this cohort of students consistently misunderstands

## Chapter Text
{{.ChapterText}}`))

// promptData is the data passed to distillPromptTmpl.
type promptData struct {
	CourseObjectives []config.CourseObjective
	ChapterText      string
}

// buildPrompt renders the LLM distillation prompt.
func buildPrompt(objectives []config.CourseObjective, chapterText string) (string, error) {
	var buf bytes.Buffer
	if err := distillPromptTmpl.Execute(&buf, promptData{
		CourseObjectives: objectives,
		ChapterText:      chapterText,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
