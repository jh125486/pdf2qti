// Package distill provides LLM-based distillation of PDF chapter text into
// structured JSON context files consumed by the generate and page commands.
package distill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
)

// Objective is a course objective that applies to this chapter.
type Objective struct {
	CO   int    `json:"co"`
	Text string `json:"text"`
}

// reCODigits extracts the digits from a co value like "1", " 2 ", or "CO1" — LLM responses are
// inconsistent about exactly how they format this field even within the same prompt template.
var reCODigits = regexp.MustCompile(`\d+`)

// UnmarshalJSON accepts co as a JSON number, a numeric string, or a string prefixed/suffixed
// with non-digit text (e.g. "CO1"); it extracts the first run of digits in the latter case.
func (o *Objective) UnmarshalJSON(data []byte) error {
	var aux struct {
		CO   any    `json:"co"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	switch v := aux.CO.(type) {
	case float64:
		o.CO = int(v)
	case string:
		digits := reCODigits.FindString(v)
		if digits == "" {
			return fmt.Errorf("objective.co: no digits found in %q", v)
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return fmt.Errorf("objective.co: cannot parse %q as int: %w", v, err)
		}
		o.CO = n
	default:
		return fmt.Errorf("objective.co: unexpected type %T", v)
	}
	o.Text = aux.Text
	return nil
}

// Slide is a single content slide's title and body, one per major section.
type Slide struct {
	Title   string `json:"title"`
	Content string `json:"content"`        // newline-separated bullet lines
	Tag     string `json:"tag,omitempty"`  // chapter tag (or "summary") from the proto-deck's meta marker
}

// VocabTerm is a single vocabulary term and its definition.
type VocabTerm struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

// vocabList unmarshals a "vocabulary" value that's expected to be an array of
// {term, definition} objects, but tolerates a flat {term: definition} object, or a bare array of
// term strings (definition left empty), too — LLM responses aren't always consistent about which
// shape they emit for this field, even within the same prompt template across calls.
type vocabList []VocabTerm

func (v *vocabList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] == '[' {
		var arr []VocabTerm
		if err := json.Unmarshal(data, &arr); err == nil {
			*v = arr
			return nil
		}
		var terms []string
		if err := json.Unmarshal(data, &terms); err != nil {
			return fmt.Errorf("vocabulary: expected array of objects or strings: %w", err)
		}
		list := make([]VocabTerm, len(terms))
		for i, t := range terms {
			list[i] = VocabTerm{Term: t}
		}
		*v = list
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("vocabulary: expected array or object: %w", err)
	}
	list := make([]VocabTerm, 0, len(m))
	for term, def := range m {
		list = append(list, VocabTerm{Term: term, Definition: def})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Term < list[j].Term })
	*v = list
	return nil
}

// Theorem is a named theorem or law and its statement, for math-heavy chapters.
type Theorem struct {
	Name      string `json:"name"`
	Statement string `json:"statement"`
}

// Section is one of a chapter's major internal subsections and its summary.
type Section struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// sectionList unmarshals a "sections" value that's expected to be an array of
// {title, summary} objects, but tolerates a flat {title: summary} object, or a bare array of
// title strings (summary left empty), too, for the same reason vocabList does.
type sectionList []Section

func (s *sectionList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] == '[' {
		var arr []Section
		if err := json.Unmarshal(data, &arr); err == nil {
			*s = arr
			return nil
		}
		var titles []string
		if err := json.Unmarshal(data, &titles); err != nil {
			return fmt.Errorf("sections: expected array of objects or strings: %w", err)
		}
		list := make([]Section, len(titles))
		for i, t := range titles {
			list[i] = Section{Title: t}
		}
		*s = list
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("sections: expected array or object: %w", err)
	}
	list := make([]Section, 0, len(m))
	for title, summary := range m {
		list = append(list, Section{Title: title, Summary: summary})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Title < list[j].Title })
	*s = list
	return nil
}

// DistilledContext holds the structured output produced by the distill command.
type DistilledContext struct {
	SourceID         string      `json:"source_id"`
	Book             string      `json:"book"`
	Chapter          int         `json:"chapter"`
	ModuleName       string      `json:"module_name"`
	Text             string      `json:"text"`
	Overview         string      `json:"overview"`
	KeyConcepts      []string    `json:"key_concepts"`
	MaterialOverview string      `json:"material_overview"`
	TeachingNotes    string      `json:"teaching_notes"`
	Objectives       []Objective `json:"objectives"`
	Vocabulary       vocabList   `json:"vocabulary,omitempty"`
	Theorems         []Theorem   `json:"theorems,omitempty"`
	Sections         sectionList `json:"sections,omitempty"`
	// Agenda and Slides feed the single-chapter pptx pipeline (internal/pptx). The standard
	// distill LLM prompt no longer populates them directly; slide planning now happens at the
	// multi-chapter proto-deck level (see GenerateProtoDeck) and is not yet wired back into
	// these per-chapter fields — until that wiring lands, callers must populate them manually.
	Agenda []string `json:"agenda"` // 3-8 top-level topics, one per major section
	Slides []Slide  `json:"slides"` // one per Content slide to generate
	// VerificationWarnings lists contradictions or issues verifyConsistency found but couldn't
	// auto-resolve (e.g. a formula stated two different ways in different parts of Text). Only
	// populated when the chapter was large enough to require chunked distillation; empty
	// otherwise since there's no cross-chunk inconsistency risk to check in a single LLM call.
	VerificationWarnings []string `json:"verification_warnings,omitempty"`
}

// Load reads and parses a DistilledContext from a JSON file at path.
func Load(path string) (*DistilledContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read context %q: %w", path, err)
	}
	var dc DistilledContext
	if err := json.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("parse context %q: %w", path, err)
	}
	return &dc, nil
}

// Save writes dc as JSON to path, creating or truncating the file.
func Save(path string, dc *DistilledContext) error {
	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write context %q: %w", path, err)
	}
	return nil
}
