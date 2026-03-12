package qti_test

import (
	"fmt"
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
	if len(item.ItemBody.RespDecls) == 0 || item.ItemBody.RespDecls[0].RCardinality != "Multiple" {
		t.Errorf("expected Multiple cardinality for MA, got %q", item.ItemBody.RespDecls[0].RCardinality)
	}
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.And == nil {
		t.Fatal("expected And condition for MA with multiple correct answers")
	}
	if len(cond.ConditionVar.And.VarEquals) != 2 {
		t.Errorf("expected 2 VarEquals in And condition, got %d", len(cond.ConditionVar.And.VarEquals))
	}
}

func TestBuildAssessment_SAQuestion(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "SA Test",
		SAQuestions: []render.Question{
			{Number: 1, Text: "What is the capital of France?", Options: []render.Option{
				{Text: "Paris", IsCorrect: true},
				{Text: "Paris, France", IsCorrect: true},
			}},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	if item.ItemBody.RespStr == nil {
		t.Fatal("expected response_str for SA question")
	}
	if item.ItemBody.RespStr.RCardinality != "Single" {
		t.Errorf("expected Single cardinality, got %q", item.ItemBody.RespStr.RCardinality)
	}
	if len(item.ResForm.ResCondition) != 2 {
		t.Errorf("expected 2 conditions for 2 accepted answers, got %d", len(item.ResForm.ResCondition))
	}
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.VarEqual == nil {
		t.Fatal("expected VarEqual condition for SA")
	}
	if cond.ConditionVar.VarEqual.Case != "No" {
		t.Errorf("expected case-insensitive match (Case=No), got %q", cond.ConditionVar.VarEqual.Case)
	}
	if cond.ConditionVar.VarEqual.Value != "Paris" {
		t.Errorf("expected answer %q, got %q", "Paris", cond.ConditionVar.VarEqual.Value)
	}
}

func TestBuildAssessment_SAQuestion_NoAnswer(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "SA No Answer",
		SAQuestions: []render.Question{
			{Number: 1, Text: "What?"},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for SA question with no answers")
	}
}

func TestBuildAssessment_ESQuestion(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "ES Test",
		ESQuestions: []render.Question{
			{Number: 1, Text: "Describe photosynthesis."},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	if item.ItemBody.RespStr == nil {
		t.Fatal("expected response_str for ES question")
	}
	if item.ItemBody.RespStr.Render.Prompt != "Box" {
		t.Errorf("expected Prompt=Box for essay, got %q", item.ItemBody.RespStr.Render.Prompt)
	}
	if len(item.ResForm.ResCondition) != 1 {
		t.Errorf("expected 1 condition for essay, got %d", len(item.ResForm.ResCondition))
	}
	if item.ResForm.ResCondition[0].ConditionVar.Other == nil {
		t.Fatal("expected Other condition for essay")
	}
	if item.ResForm.ResCondition[0].SetVar.Value != "0" {
		t.Errorf("expected SCORE=0 for essay, got %q", item.ResForm.ResCondition[0].SetVar.Value)
	}
}

func TestBuildAssessment_MTQuestion(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "MT Test",
		MTQuestions: []render.Question{
			{Number: 1, Text: "Match countries to capitals.", Options: []render.Option{
				{Text: "France", IsCorrect: true, MatchText: "Paris"},
				{Text: "Germany", IsCorrect: true, MatchText: "Berlin"},
				{Text: "Spain", IsCorrect: true, MatchText: "Madrid"},
				{Text: "Italy", IsCorrect: true, MatchText: "Rome"},
			}},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	if len(item.ItemBody.RespDecls) != 4 {
		t.Errorf("expected 4 response_lid for 4 pairs, got %d", len(item.ItemBody.RespDecls))
	}
	if len(item.ResForm.ResCondition) != 4 {
		t.Errorf("expected 4 conditions for 4 pairs, got %d", len(item.ResForm.ResCondition))
	}
	// Each pair should add 25 points (100/4)
	for i, cond := range item.ResForm.ResCondition {
		if cond.SetVar.Action != "Add" {
			t.Errorf("condition %d: expected Add action, got %q", i, cond.SetVar.Action)
		}
		if cond.SetVar.Value != "25" {
			t.Errorf("condition %d: expected score 25, got %q", i, cond.SetVar.Value)
		}
	}
}

func TestBuildAssessment_MTQuestion_NoPairs(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "MT No Pairs",
		MTQuestions: []render.Question{
			{Number: 1, Text: "Match?"},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for MT question with no pairs")
	}
}

func TestBuildAssessment_NRQuestion_ExactMatch(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "NR Test Exact",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What is 2+2?", Options: []render.Option{
				{Text: "4", IsCorrect: true},
			}},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	if item.ItemBody.RespNum == nil {
		t.Fatal("expected response_num for NR question")
	}
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.VarEqual == nil {
		t.Fatal("expected VarEqual for exact NR match")
	}
	if cond.ConditionVar.VarEqual.Value != "4" {
		t.Errorf("expected answer 4, got %q", cond.ConditionVar.VarEqual.Value)
	}
}

func TestBuildAssessment_NRQuestion_WithTolerance(t *testing.T) {
	var answer float64 = 3.14
	var tolerance float64 = 0.005
	draft := &render.QuizDraft{
		Title: "NR Test Tolerance",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What is π to 2 decimal places?", Options: []render.Option{
				{Text: fmt.Sprintf("%g", answer), IsCorrect: true},
				{Text: fmt.Sprintf("%g", tolerance), IsCorrect: false},
			}},
		},
	}
	a, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := a.Assessment.Sections[0].Items[0]
	cond := item.ResForm.ResCondition[0]
	if cond.ConditionVar.VarGTE == nil || cond.ConditionVar.VarLTE == nil {
		t.Fatal("expected VarGTE and VarLTE for NR with tolerance")
	}
	wantLo := fmt.Sprintf("%g", answer-tolerance)
	wantHi := fmt.Sprintf("%g", answer+tolerance)
	if cond.ConditionVar.VarGTE.Value != wantLo {
		t.Errorf("expected lower bound %q, got %q", wantLo, cond.ConditionVar.VarGTE.Value)
	}
	if cond.ConditionVar.VarLTE.Value != wantHi {
		t.Errorf("expected upper bound %q, got %q", wantHi, cond.ConditionVar.VarLTE.Value)
	}
}

func TestBuildAssessment_NRQuestion_NoAnswer(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "NR No Answer",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What?"},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for NR question with no answer")
	}
}

func TestMarshal_AllTypes(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "All Types Quiz",
		TFQuestions: []render.Question{
			{Number: 1, Text: "True or false?", Options: []render.Option{
				{Text: "True", IsCorrect: true},
				{Text: "False", IsCorrect: false},
			}},
		},
		SAQuestions: []render.Question{
			{Number: 2, Text: "Capital of France?", Options: []render.Option{
				{Text: "Paris", IsCorrect: true},
			}},
		},
		ESQuestions: []render.Question{
			{Number: 3, Text: "Essay question."},
		},
		MTQuestions: []render.Question{
			{Number: 4, Text: "Match.", Options: []render.Option{
				{Text: "A", IsCorrect: true, MatchText: "1"},
				{Text: "B", IsCorrect: true, MatchText: "2"},
			}},
		},
		NRQuestions: []render.Question{
			{Number: 5, Text: "What is 2+2?", Options: []render.Option{
				{Text: "4", IsCorrect: true},
				{Text: "0.5", IsCorrect: false}, // tolerance
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
	for _, want := range []string{
		"response_lid", "response_str", "response_num",
		"render_fib", "render_choice",
		"varequal", "vargte", "varlte", "other",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("missing XML element %q", want)
		}
	}
}

func TestBuildAssessment_TFQuestion_NoCorrectOption(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "TF No Correct",
		TFQuestions: []render.Question{
			{Number: 1, Text: "True or false?", Options: []render.Option{
				{Text: "True", IsCorrect: false},
				{Text: "False", IsCorrect: false},
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for TF question with no correct option")
	}
}

func TestBuildAssessment_MAQuestion_NoCorrectOption(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "MA No Correct",
		MAQuestions: []render.Question{
			{Number: 1, Text: "Select all correct?", Options: []render.Option{
				{Text: "A", IsCorrect: false},
				{Text: "B", IsCorrect: false},
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for MA question with no correct option")
	}
}

func TestBuildAssessment_NRQuestion_InvalidAnswerValue(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "NR Invalid Answer",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What?", Options: []render.Option{
				{Text: "not-a-number", IsCorrect: true},
				{Text: "0.5", IsCorrect: false}, // tolerance present → triggers range path
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for non-numeric NR answer value")
	}
}

func TestBuildAssessment_NRQuestion_NoAnswerValue(t *testing.T) {
	// All options are tolerance-only (IsCorrect=false) → answerVal remains ""
	draft := &render.QuizDraft{
		Title: "NR No Answer Value",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What?", Options: []render.Option{
				{Text: "0.5", IsCorrect: false},
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for NR question with no correct answer value")
	}
}

func TestBuildAssessment_NRQuestion_InvalidTolerance(t *testing.T) {
	draft := &render.QuizDraft{
		Title: "NR Invalid Tolerance",
		NRQuestions: []render.Question{
			{Number: 1, Text: "What?", Options: []render.Option{
				{Text: "42", IsCorrect: true},
				{Text: "not-a-number", IsCorrect: false}, // bad tolerance
			}},
		},
	}
	_, err := qti.BuildAssessment(draft)
	if err == nil {
		t.Fatal("expected error for non-numeric NR tolerance value")
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
