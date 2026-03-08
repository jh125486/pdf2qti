package qti_test

import (
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

func TestBuildAssessment_NoTitle(t *testing.T) {
	draft := &render.QuizDraft{}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestBuildAssessment_Basic(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "Sample Quiz",
		TFQuestions: []render.Question{
			{Number: 1, Text: "Is Go fun?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: false},
			}},
		},
		MCQuestions: []render.Question{
			{Number: 2, Text: "What is 1+1?", Options: []render.Option{
				{Text: "1", IsCorrect: false},
				{Text: "2", IsCorrect: true},
				{Text: "3", IsCorrect: false},
			}},
		},
	}

	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.Assessment.Sections) != 1 {
		t.Errorf("expected 1 section, got %d", len(a.Assessment.Sections))
	}
	if len(a.Assessment.Sections[0].Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(a.Assessment.Sections[0].Items))
	}
}

func TestMarshal_ProducesXML(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "XML Test Quiz",
		MCQuestions: []render.Question{
			{Number: 1, Text: "Q?", Options: []render.Option{
				{Text: "A", IsCorrect: true},
				{Text: "B", IsCorrect: false},
			}},
		},
	}

	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	xmlBytes, err := qti.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	xmlStr := string(xmlBytes)
	if !strings.Contains(xmlStr, "<?xml") {
		t.Error("missing XML header")
	}
	if !strings.Contains(xmlStr, "questestinterop") {
		t.Error("missing questestinterop element")
	}
	if !strings.Contains(xmlStr, "XML Test Quiz") {
		t.Error("missing quiz title")
	}
	if !strings.Contains(xmlStr, "Q?") {
		t.Error("missing question text")
	}
}

func TestBuildAssessment_CorrectAnswerMapping(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "Answer Test",
		MCQuestions: []render.Question{
			{Number: 1, Text: "Which?", Options: []render.Option{
				{Text: "Wrong", IsCorrect: false},
				{Text: "Right", IsCorrect: true},
				{Text: "Also Wrong", IsCorrect: false},
			}},
		},
	}

	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	item := a.Assessment.Sections[0].Items[0]
	// Correct answer should be q1_c2 (second option is correct)
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.VarEqual == nil {
		t.Fatal("expected VarEqual condition")
	}
	if cond.ConditionVar.VarEqual.Value != "q1_c2" {
		t.Errorf("expected correct ident q1_c2, got %q", cond.ConditionVar.VarEqual.Value)
	}
}

func TestBuildAssessment_MAQuestion_MultipleCorrect(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "MA Test",
		MAQuestions: []render.Question{
			{Number: 1, Text: "Select all correct answers", Options: []render.Option{
				{Text: "Correct A", IsCorrect: true},
				{Text: "Wrong B", IsCorrect: false},
				{Text: "Correct C", IsCorrect: true},
			}},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	if item.ItemBody.RespDecl.RCardinality != "Multiple" {
		t.Errorf("expected Multiple cardinality for MA, got %q", item.ItemBody.RespDecl.RCardinality)
	}
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.And == nil {
		t.Fatal("expected And condition for MA with multiple correct answers")
	}
	if len(cond.ConditionVar.And.VarEquals) != 2 {
		t.Errorf("expected 2 VarEquals in And condition, got %d", len(cond.ConditionVar.And.VarEquals))
	}
}

func TestBuildAssessment_NoCorrectOption(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "No Correct",
		MCQuestions: []render.Question{
			{Number: 1, Text: "Question?", Options: []render.Option{
				{Text: "A", IsCorrect: false},
				{Text: "B", IsCorrect: false},
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for question with no correct option")
	}
}
