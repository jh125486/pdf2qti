// Package render provides markdown rendering utilities for quiz drafts.
package render

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// QuizDraft represents a complete quiz draft in markdown format.
type QuizDraft struct {
	Title       string
	Description string
	TFQuestions []Question
	MAQuestions []Question
	MCQuestions []Question
	SAQuestions []Question // Short Answer (fill-in-the-blank): options use [=] for acceptable answers
	ESQuestions []Question // Essay: open-ended, no options, manually graded
	MTQuestions []Question // Matching: options use [>] with "left = right" pairs
	NRQuestions []Question // Numerical: options use [=] for the answer value, [~] for tolerance
}

// Question represents a single quiz question.
type Question struct {
	Number  int
	Text    string
	Options []Option
}

// Option represents a single answer option.
type Option struct {
	Text      string
	IsCorrect bool
	MatchText string // for matching questions: the right-hand side of the pair
}

// RenderDraft renders a QuizDraft to markdown format.
func RenderDraft(d *QuizDraft) (string, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# %s\n\n", d.Title)
	if d.Description != "" {
		fmt.Fprintf(&buf, "%s\n\n", d.Description)
	}

	if len(d.TFQuestions) > 0 {
		fmt.Fprintf(&buf, "## TF\n\n")
		for _, q := range d.TFQuestions {
			renderQuestion(&buf, q)
		}
	}
	if len(d.MAQuestions) > 0 {
		fmt.Fprintf(&buf, "## MA\n\n")
		for _, q := range d.MAQuestions {
			renderQuestion(&buf, q)
		}
	}
	if len(d.MCQuestions) > 0 {
		fmt.Fprintf(&buf, "## MC\n\n")
		for _, q := range d.MCQuestions {
			renderQuestion(&buf, q)
		}
	}
	if len(d.SAQuestions) > 0 {
		fmt.Fprintf(&buf, "## SA\n\n")
		for _, q := range d.SAQuestions {
			renderSAQuestion(&buf, q)
		}
	}
	if len(d.ESQuestions) > 0 {
		fmt.Fprintf(&buf, "## ES\n\n")
		for _, q := range d.ESQuestions {
			renderESQuestion(&buf, q)
		}
	}
	if len(d.MTQuestions) > 0 {
		fmt.Fprintf(&buf, "## MT\n\n")
		for _, q := range d.MTQuestions {
			renderMTQuestion(&buf, q)
		}
	}
	if len(d.NRQuestions) > 0 {
		fmt.Fprintf(&buf, "## NR\n\n")
		for _, q := range d.NRQuestions {
			renderNRQuestion(&buf, q)
		}
	}
	return buf.String(), nil
}

func renderQuestion(buf *bytes.Buffer, q Question) {
	fmt.Fprintf(buf, "%d. %s\n", q.Number, q.Text)
	for _, o := range q.Options {
		if o.IsCorrect {
			fmt.Fprintf(buf, "   [*] %s\n", o.Text)
		} else {
			fmt.Fprintf(buf, "   [ ] %s\n", o.Text)
		}
	}
	fmt.Fprintln(buf)
}

func renderSAQuestion(buf *bytes.Buffer, q Question) {
	fmt.Fprintf(buf, "%d. %s\n", q.Number, q.Text)
	for _, o := range q.Options {
		fmt.Fprintf(buf, "   [=] %s\n", o.Text)
	}
	fmt.Fprintln(buf)
}

func renderESQuestion(buf *bytes.Buffer, q Question) {
	fmt.Fprintf(buf, "%d. %s\n", q.Number, q.Text)
	fmt.Fprintln(buf)
}

func renderMTQuestion(buf *bytes.Buffer, q Question) {
	fmt.Fprintf(buf, "%d. %s\n", q.Number, q.Text)
	for _, o := range q.Options {
		fmt.Fprintf(buf, "   [>] %s = %s\n", o.Text, o.MatchText)
	}
	fmt.Fprintln(buf)
}

func renderNRQuestion(buf *bytes.Buffer, q Question) {
	fmt.Fprintf(buf, "%d. %s\n", q.Number, q.Text)
	for _, o := range q.Options {
		if o.IsCorrect {
			fmt.Fprintf(buf, "   [=] %s\n", o.Text)
		} else {
			fmt.Fprintf(buf, "   [~] %s\n", o.Text)
		}
	}
	fmt.Fprintln(buf)
}

// ParseDraft parses a markdown quiz draft.
// Format:
//
//	# Title
//	optional description
//	## TF / ## MA / ## MC / ## SA / ## ES / ## MT / ## NR
//	N. Question text
//	   [*] correct          (TF/MA/MC)
//	   [ ] wrong            (TF/MA/MC)
//	   [=] answer           (SA: acceptable answer; NR: numeric value)
//	   [~] tolerance        (NR: tolerance around the numeric answer)
//	   [>] left = right     (MT: matching pair)
func ParseDraft(md string) (*QuizDraft, error) {
	d := &QuizDraft{}
	lines := strings.Split(md, "\n")
	var section string
	var currentQ *Question
	var descLines []string
	seenFirstSection := false

	flush := func() {
		if currentQ == nil {
			return
		}
		switch section {
		case "TF":
			d.TFQuestions = append(d.TFQuestions, *currentQ)
		case "MA":
			d.MAQuestions = append(d.MAQuestions, *currentQ)
		case "MC":
			d.MCQuestions = append(d.MCQuestions, *currentQ)
		case "SA":
			d.SAQuestions = append(d.SAQuestions, *currentQ)
		case "ES":
			d.ESQuestions = append(d.ESQuestions, *currentQ)
		case "MT":
			d.MTQuestions = append(d.MTQuestions, *currentQ)
		case "NR":
			d.NRQuestions = append(d.NRQuestions, *currentQ)
		}
		currentQ = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && d.Title == "" {
			d.Title = strings.TrimPrefix(trimmed, "# ")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			if !seenFirstSection {
				d.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
				descLines = nil
				seenFirstSection = true
			}
			section = strings.TrimPrefix(trimmed, "## ")
			continue
		}
		if d.Title != "" && !seenFirstSection && trimmed != "" {
			descLines = append(descLines, trimmed)
			continue
		}
		if section == "" {
			continue
		}
		// Question line: "N. text"
		if q, ok := parseQuestionLine(trimmed); ok {
			flush()
			currentQ = &q
			continue
		}
		// Option line
		if currentQ != nil {
			if opt, ok := parseOptionLine(trimmed); ok {
				currentQ.Options = append(currentQ.Options, opt)
			}
		}
	}
	flush()
	return d, nil
}

func parseQuestionLine(s string) (Question, bool) {
	dot := strings.Index(s, ". ")
	if dot < 1 {
		return Question{}, false
	}
	numStr := s[:dot]
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return Question{}, false
	}
	text := strings.TrimSpace(s[dot+2:])
	return Question{Number: n, Text: text}, true
}

func parseOptionLine(s string) (Option, bool) {
	if strings.HasPrefix(s, "[*] ") {
		return Option{Text: strings.TrimPrefix(s, "[*] "), IsCorrect: true}, true
	}
	if strings.HasPrefix(s, "[ ] ") {
		return Option{Text: strings.TrimPrefix(s, "[ ] "), IsCorrect: false}, true
	}
	if strings.HasPrefix(s, "[=] ") {
		return Option{Text: strings.TrimPrefix(s, "[=] "), IsCorrect: true}, true
	}
	if strings.HasPrefix(s, "[~] ") {
		return Option{Text: strings.TrimPrefix(s, "[~] "), IsCorrect: false}, true
	}
	if strings.HasPrefix(s, "[>] ") {
		pair := strings.TrimPrefix(s, "[>] ")
		idx := strings.Index(pair, " = ")
		if idx >= 0 {
			return Option{Text: pair[:idx], IsCorrect: true, MatchText: pair[idx+3:]}, true
		}
	}
	return Option{}, false
}

// ExecuteTemplate renders a Go text/template with the given data.
func ExecuteTemplate(tmplStr string, data any) (string, error) {
	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
