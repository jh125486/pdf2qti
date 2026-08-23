package qti_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jh125486/pdf2qti/internal/qti"
	"github.com/jh125486/pdf2qti/internal/render"
)

// wrapCodeSpansResult carries the outcome of a BuildAssessment+Marshal run
// back from the goroutine that computed it, so the test goroutine (never the
// spawned one) can perform t.Fatalf/t.Errorf assertions on the result.
type wrapCodeSpansResult struct {
	xml []byte
	err error
}

func sampleDraft() *render.QuizDraft {
	return &render.QuizDraft{
		Title: "Signals Quiz",
		TFQuestions: []render.Question{
			{Number: 1, Text: "Signals are synchronous by default?", Options: []render.Option{{Text: "True", IsCorrect: false}, {Text: "False", IsCorrect: true}}},
		},
		MAQuestions: []render.Question{
			{Number: 2, Text: "Select async-signal-safe operations", Options: []render.Option{{Text: "write", IsCorrect: true}, {Text: "printf", IsCorrect: false}}},
		},
		MCQuestions: []render.Question{
			{Number: 3, Text: "Ctrl-C sends?", Options: []render.Option{{Text: "SIGINT", IsCorrect: true}, {Text: "SIGTERM", IsCorrect: false}}},
		},
		SAQuestions: []render.Question{
			{Number: 4, Text: "Name one ignored signal", Options: []render.Option{{Text: "SIGPIPE", IsCorrect: true}}},
		},
		ESQuestions: []render.Question{
			{Number: 5, Text: "Explain signal dispositions."},
		},
		MTQuestions: []render.Question{
			{Number: 6, Text: "Match signal to action", Options: []render.Option{{Text: "SIGINT", MatchText: "Terminate", IsCorrect: true}}},
		},
		NRQuestions: []render.Question{
			{Number: 7, Text: "How many realtime signals?", Options: []render.Option{{Text: "32", IsCorrect: true}, {Text: "1", IsCorrect: false}}},
		},
	}
}

func TestBuildAssessment_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		draft   *render.QuizDraft
		wantErr bool
		errLike string
		verify  func(t *testing.T, a *qti.Assessment)
	}{
		{name: "success", draft: sampleDraft(), verify: verifySampleAssessment},
		{name: "missing title", draft: &render.QuizDraft{}, wantErr: true, errLike: "must have a title"},
		{name: "bad tf no correct option", draft: &render.QuizDraft{Title: "Bad", TFQuestions: []render.Question{{Number: 1, Text: "Q", Options: []render.Option{{Text: "A", IsCorrect: false}}}}}, wantErr: true, errLike: "has no correct option"},
		{
			name: "MA with multiple correct answers",
			draft: &render.QuizDraft{Title: "MA Multi", MAQuestions: []render.Question{
				{Number: 1, Text: "Pick two", Options: []render.Option{
					{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: true}, {Text: "C", IsCorrect: false},
				}},
			}},
			verify: verifyMAMultipleCorrect,
		},
		{
			name:    "SA no options",
			draft:   &render.QuizDraft{Title: "SA Bad", SAQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no accepted answers",
		},
		{
			name:    "MT no options",
			draft:   &render.QuizDraft{Title: "MT Bad", MTQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no pairs",
		},
		{
			name:    "NR no options",
			draft:   &render.QuizDraft{Title: "NR Bad", NRQuestions: []render.Question{{Number: 1, Text: "Q"}}},
			wantErr: true, errLike: "has no answer",
		},
		{
			name: "NR without tolerance",
			draft: &render.QuizDraft{Title: "NR Single", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "42", IsCorrect: true}}},
			}},
		},
		{
			name: "NR invalid answer value",
			draft: &render.QuizDraft{Title: "NR Bad Answer", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "not-a-number", IsCorrect: true}, {Text: "1", IsCorrect: false}}},
			}},
			wantErr: true, errLike: "invalid answer value",
		},
		{
			name: "NR invalid tolerance value",
			draft: &render.QuizDraft{Title: "NR Bad Tolerance", NRQuestions: []render.Question{
				{Number: 1, Text: "Value?", Options: []render.Option{{Text: "42", IsCorrect: true}, {Text: "not-a-number", IsCorrect: false}}},
			}},
			wantErr: true, errLike: "invalid tolerance value",
		},
		{
			name:  "ES only draft",
			draft: &render.QuizDraft{Title: "Essay Only", ESQuestions: []render.Question{{Number: 1, Text: "Explain."}}},
		},
		{
			name: "sequential item index arithmetic across all types",
			draft: &render.QuizDraft{
				Title: "Sequential",
				TFQuestions: []render.Question{
					{Number: 1, Text: "TF1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
					{Number: 2, Text: "TF2", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 3, Text: "MA1", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
					{Number: 4, Text: "MA2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				MCQuestions: []render.Question{
					{Number: 5, Text: "MC1", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
					{Number: 6, Text: "MC2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				SAQuestions: []render.Question{
					{Number: 7, Text: "SA1", Options: []render.Option{{Text: "answer1", IsCorrect: true}}},
					{Number: 8, Text: "SA2", Options: []render.Option{{Text: "answer2", IsCorrect: true}}},
				},
				ESQuestions: []render.Question{
					{Number: 9, Text: "ES1"},
					{Number: 10, Text: "ES2"},
				},
				MTQuestions: []render.Question{
					{Number: 11, Text: "MT1", Options: []render.Option{{Text: "Left1", MatchText: "Right1", IsCorrect: true}}},
					{Number: 12, Text: "MT2", Options: []render.Option{{Text: "Left2", MatchText: "Right2", IsCorrect: true}}},
				},
				NRQuestions: []render.Question{
					{Number: 13, Text: "NR1", Options: []render.Option{{Text: "1", IsCorrect: true}, {Text: "0.5", IsCorrect: false}}},
					{Number: 14, Text: "NR2", Options: []render.Option{{Text: "2", IsCorrect: true}, {Text: "0.5", IsCorrect: false}}},
				},
			},
			verify: verifySequentialItemIndices,
		},
		{
			name: "TF second question no correct option",
			draft: &render.QuizDraft{Title: "TF Err", TFQuestions: []render.Question{
				{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: false}}},
			}},
			wantErr: true, errLike: "build TF item 2",
		},
		{
			name: "MA no correct option",
			draft: &render.QuizDraft{
				Title: "MA Err",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: false}}},
				},
			},
			wantErr: true, errLike: "build MA item 2",
		},
		{
			name: "MC no correct option",
			draft: &render.QuizDraft{
				Title: "MC Err",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				MCQuestions: []render.Question{
					{Number: 3, Text: "Q3", Options: []render.Option{{Text: "A", IsCorrect: false}}},
				},
			},
			wantErr: true, errLike: "build MC item 3",
		},
		{
			name: "SA no options at item 4",
			draft: &render.QuizDraft{
				Title: "SA Err",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				MCQuestions: []render.Question{
					{Number: 3, Text: "Q3", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				SAQuestions: []render.Question{
					{Number: 4, Text: "Q4"},
				},
			},
			wantErr: true, errLike: "build SA item 4",
		},
		{
			name: "MT no pairs at item 6",
			draft: &render.QuizDraft{
				Title: "MT Err",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				MCQuestions: []render.Question{
					{Number: 3, Text: "Q3", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				SAQuestions: []render.Question{
					{Number: 4, Text: "Q4", Options: []render.Option{{Text: "answer", IsCorrect: true}}},
				},
				ESQuestions: []render.Question{
					{Number: 5, Text: "Q5"},
				},
				MTQuestions: []render.Question{
					{Number: 6, Text: "Q6"},
				},
			},
			wantErr: true, errLike: "build MT item 6",
		},
		{
			name: "NR no answer at item 7",
			draft: &render.QuizDraft{
				Title: "NR Err",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Q1", Options: []render.Option{{Text: "True", IsCorrect: true}, {Text: "False", IsCorrect: false}}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Q2", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				MCQuestions: []render.Question{
					{Number: 3, Text: "Q3", Options: []render.Option{{Text: "A", IsCorrect: true}, {Text: "B", IsCorrect: false}}},
				},
				SAQuestions: []render.Question{
					{Number: 4, Text: "Q4", Options: []render.Option{{Text: "answer", IsCorrect: true}}},
				},
				ESQuestions: []render.Question{
					{Number: 5, Text: "Q5"},
				},
				MTQuestions: []render.Question{
					{Number: 6, Text: "Q6", Options: []render.Option{{Text: "Left", MatchText: "Right", IsCorrect: true}}},
				},
				NRQuestions: []render.Question{
					{Number: 7, Text: "Q7"},
				},
			},
			wantErr: true, errLike: "build NR item 7",
		},
		{
			name: "MT with three match pairs",
			draft: &render.QuizDraft{
				Title: "MT Multi",
				MTQuestions: []render.Question{
					{Number: 1, Text: "Match signal to action", Options: []render.Option{
						{Text: "SIGINT", MatchText: "Terminate", IsCorrect: true},
						{Text: "SIGKILL", MatchText: "Force kill", IsCorrect: true},
						{Text: "SIGSTOP", MatchText: "Pause", IsCorrect: true},
					}},
				},
			},
			verify: verifyMTThreePairs,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assessment, err := qti.BuildAssessment(tt.draft)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if assessment != nil {
				if assessment.Assessment.Title == "" {
					t.Fatal("expected non-empty assessment title")
				}
				if len(assessment.Assessment.Sections) == 0 {
					t.Fatal("expected at least one section")
				}
			}
			if tt.verify != nil {
				tt.verify(t, assessment)
			}
		})
	}
}

// verifySampleAssessment asserts on QTI semantics that are easy to silently regress:
// choice ident mapping (qN_cM), SA case-insensitive matching, NR tolerance producing
// VarGTE/VarLTE bounds, and MT scoring/actions per pair.
func verifySampleAssessment(t *testing.T, a *qti.Assessment) {
	t.Helper()
	items := a.Assessment.Sections[0].Items
	if len(items) != 7 {
		t.Fatalf("expected 7 items, got %d", len(items))
	}

	// TF (q1): correct answer is the 2nd option ("False") -> q1_c2.
	tf := items[0]
	if got := tf.ResForm.ResCondition[0].ConditionVar.VarEqual; got == nil || got.Value != "q1_c2" {
		t.Errorf("TF: expected correct ident q1_c2, got %+v", got)
	}

	// MA (q2): a single correct option ("write") must produce a bare
	// VarEqual condition, not an AndCondition, and its value must match the
	// single correct option's generated ident (q2_c1).
	verifyMASingleCorrect(t, &items[1])

	// MC (q3): correct answer is the 1st option ("SIGINT") -> q3_c1.
	mc := items[2]
	if got := mc.ResForm.ResCondition[0].ConditionVar.VarEqual; got == nil || got.Value != "q3_c1" {
		t.Errorf("MC: expected correct ident q3_c1, got %+v", got)
	}

	// SA (q4): matching must be case-insensitive.
	sa := items[3]
	if got := sa.ResForm.ResCondition[0].ConditionVar.VarEqual; got == nil || got.Case != "No" {
		t.Errorf("SA: expected case-insensitive VarEqual (case=No), got %+v", got)
	}

	// MT (q6): single pair should score the full 100 points via an Add action.
	mt := items[5]
	if len(mt.ResForm.ResCondition) != 1 || mt.ResForm.ResCondition[0].SetVar.Action != "Add" || mt.ResForm.ResCondition[0].SetVar.Value != "100" {
		t.Errorf("MT: expected single Add-100 scoring condition, got %+v", mt.ResForm.ResCondition)
	}

	// NR (q7): answer=32, tolerance=1 -> bounds [31, 33] via VarGTE/VarLTE.
	nr := items[6]
	cv := nr.ResForm.ResCondition[0].ConditionVar
	if cv.VarGTE == nil || cv.VarLTE == nil {
		t.Fatalf("NR: expected VarGTE/VarLTE bounds, got %+v", cv)
	}
	if cv.VarGTE.Value != "31" || cv.VarLTE.Value != "33" {
		t.Errorf("NR: expected bounds [31,33], got [%s,%s]", cv.VarGTE.Value, cv.VarLTE.Value)
	}
}

// verifyMASingleCorrect asserts that an MA question with exactly one correct
// option produces a bare VarEqual condition (not an AndCondition) whose value
// matches that option's generated ident — the boundary-opposite of
// verifyMAMultipleCorrect.
func verifyMASingleCorrect(t *testing.T, ma *qti.Item) {
	t.Helper()
	cv := ma.ResForm.ResCondition[0].ConditionVar
	if cv.And != nil {
		t.Errorf("MA: expected no AndCondition for single correct option, got %+v", cv.And)
	}
	if cv.VarEqual == nil || cv.VarEqual.Value != "q2_c1" {
		t.Errorf("MA: expected bare VarEqual with value q2_c1, got %+v", cv.VarEqual)
	}
}

// verifyMAMultipleCorrect asserts that an MA question with multiple correct
// options produces an AndCondition of varequals (and no bare VarEqual) — the
// boundary-opposite of the single-correct-option case asserted in
// verifySampleAssessment.
func verifyMAMultipleCorrect(t *testing.T, a *qti.Assessment) {
	t.Helper()
	cv := a.Assessment.Sections[0].Items[0].ResForm.ResCondition[0].ConditionVar
	if cv.And == nil || len(cv.And.VarEquals) != 2 {
		t.Fatalf("expected AndCondition with 2 varequals, got %+v", cv)
	}
	if cv.And.VarEquals[0].Value != "q1_c1" || cv.And.VarEquals[1].Value != "q1_c2" {
		t.Errorf("expected idents q1_c1,q1_c2, got %s,%s", cv.And.VarEquals[0].Value, cv.And.VarEquals[1].Value)
	}
	if cv.VarEqual != nil {
		t.Errorf("expected bare VarEqual to be unset for multiple correct options, got %+v", cv.VarEqual)
	}
}

// verifySequentialItemIndices asserts that item idents/titles number
// sequentially (q1..q14, "Question 1".."Question 14") across all seven
// question types, two of each, regardless of per-type offsets.
func verifySequentialItemIndices(t *testing.T, a *qti.Assessment) {
	t.Helper()
	items := a.Assessment.Sections[0].Items
	if len(items) != 14 {
		t.Fatalf("expected 14 items, got %d", len(items))
	}
	for k := range 14 {
		wantIdent := fmt.Sprintf("q%d", k+1)
		wantTitle := fmt.Sprintf("Question %d", k+1)
		if items[k].Ident != wantIdent {
			t.Errorf("item %d: expected Ident %q, got %q", k, wantIdent, items[k].Ident)
		}
		if items[k].Title != wantTitle {
			t.Errorf("item %d: expected Title %q, got %q", k, wantTitle, items[k].Title)
		}
	}
}

// verifyMTThreePairs asserts on a Matching question with three pairs: the
// generated response/match idents follow the qN_resp_j / qN_match_j pattern
// for each pair, and the per-pair score is the integer-truncated 100/3 (33)
// rather than 100 — a case that single-pair MT tests can't distinguish from
// a possible *-instead-of-/ arithmetic mutant.
func verifyMTThreePairs(t *testing.T, a *qti.Assessment) {
	t.Helper()
	item := a.Assessment.Sections[0].Items[0]
	if got := len(item.ItemBody.RespDecls); got != 3 {
		t.Fatalf("expected 3 response declarations, got %d", got)
	}
	for j := 1; j <= 3; j++ {
		wantResp := fmt.Sprintf("q1_resp_%d", j)
		if item.ItemBody.RespDecls[j-1].Ident != wantResp {
			t.Errorf("resp %d: expected ident %q, got %q", j, wantResp, item.ItemBody.RespDecls[j-1].Ident)
		}
		if got := len(item.ItemBody.RespDecls[j-1].Render.Choices); got != 3 {
			t.Fatalf("resp %d: expected 3 match choices, got %d", j, got)
		}
		wantMatch := fmt.Sprintf("q1_match_%d", j)
		if item.ItemBody.RespDecls[j-1].Render.Choices[j-1].Ident != wantMatch {
			t.Errorf("resp %d: expected match ident %q at position %d, got %q", j, wantMatch, j-1, item.ItemBody.RespDecls[j-1].Render.Choices[j-1].Ident)
		}
	}
	if got := len(item.ResForm.ResCondition); got != 3 {
		t.Fatalf("expected 3 respconditions, got %d", got)
	}
	for j := 1; j <= 3; j++ {
		cond := item.ResForm.ResCondition[j-1]
		if got := cond.SetVar.Value; got != "33" {
			t.Errorf("pair %d: expected per-pair score 33 (100/3 truncated), got %s", j, got)
		}
		wantResp := fmt.Sprintf("q1_resp_%d", j)
		wantMatch := fmt.Sprintf("q1_match_%d", j)
		if cond.ConditionVar.VarEqual == nil {
			t.Fatalf("pair %d: expected VarEqual condition, got %+v", j, cond.ConditionVar)
		}
		if cond.ConditionVar.VarEqual.RespIdent != wantResp {
			t.Errorf("pair %d: expected respident %q, got %q", j, wantResp, cond.ConditionVar.VarEqual.RespIdent)
		}
		if cond.ConditionVar.VarEqual.Value != wantMatch {
			t.Errorf("pair %d: expected match value %q, got %q", j, wantMatch, cond.ConditionVar.VarEqual.Value)
		}
	}
}

func TestMarshal_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		buildAssmt func(t *testing.T) (*qti.Assessment, error)
		wantErr    bool
		errLike    string
		wantToken  []string
	}{
		{
			name: "success",
			buildAssmt: func(t *testing.T) (*qti.Assessment, error) {
				t.Helper()
				return qti.BuildAssessment(sampleDraft())
			},
			wantToken: []string{
				"<?xml", "Signals Quiz", "questestinterop",
				`xmlns="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"`,
				`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`,
				`xsi:schemaLocation="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1 http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1.xsd"`,
				"response_lid", "response_str", "response_num",
				"varequal", "vargte", "varlte",
				`respident="q1_resp"`, `case="No"`,
				`texttype="text/html"`, "<![CDATA[Signals are synchronous by default?]]>",
			},
		},
		{
			name:       "zero assessment",
			buildAssmt: func(_ *testing.T) (*qti.Assessment, error) { return &qti.Assessment{}, nil },
			wantToken: []string{
				"<?xml",
				`xmlns="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"`,
				`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`,
				`xsi:schemaLocation="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1 http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1.xsd"`,
			},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assmt, buildErr := tt.buildAssmt(t)
			if buildErr != nil {
				t.Fatalf("failed to build assessment: %v", buildErr)
			}
			xmlBytes, err := qti.Marshal(assmt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			xmlStr := string(xmlBytes)
			for _, tok := range tt.wantToken {
				if !strings.Contains(xmlStr, tok) {
					t.Fatalf("expected marshaled xml to contain %q", tok)
				}
			}
		})
	}
}

func TestMarshalPreservesLaTeXInRichText(t *testing.T) {
	t.Parallel()
	draft := &render.QuizDraft{
		Title: "Math",
		MCQuestions: []render.Question{{
			Number: 1,
			Text:   `Solve \(x^2 = 4\).`,
			Options: []render.Option{
				{Text: `\(x = 2\)`, IsCorrect: true},
				{Text: `\(x = 3\)`, IsCorrect: false},
			},
		}},
	}
	assessment, err := qti.BuildAssessment(draft)
	if err != nil {
		t.Fatal(err)
	}
	xmlBytes, err := qti.Marshal(assessment)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`texttype="text/html"`,
		`<![CDATA[Solve \(x^2 = 4\).]]>`,
		`<![CDATA[\(x = 2\)]]>`,
	} {
		if !strings.Contains(string(xmlBytes), want) {
			t.Fatalf("expected marshaled XML to contain %q: %s", want, xmlBytes)
		}
	}
}

func TestMarshalWrapsCodeSpans_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "single code span",
			text: "`create_vector(components)` returns a list.",
			want: "<code>create_vector(components)</code> returns a list.",
		},
		{
			name: "code span adjacent to LaTeX math",
			text: `For ` + "`v = create_vector([3, -1, 4])`" + `, ` + "`vector_dimension(v)`" + ` returns \(3\).`,
			want: `For <code>v = create_vector([3, -1, 4])</code>, <code>vector_dimension(v)</code> returns \(3\).`,
		},
		{
			name: "code span content is HTML-escaped",
			text: "`a < b && c`",
			want: "<code>a &lt; b &amp;&amp; c</code>",
		},
		{
			name: "double-backtick span containing a literal backtick",
			text: "Run ``foo`bar`` to see it.",
			want: "Run <code>foo`bar</code> to see it.",
		},
		{
			name: "padding space is stripped when span starts with a backtick",
			text: "`` `foo ``",
			want: "<code>`foo</code>",
		},
		{
			name: "unmatched backtick run is left as literal text",
			text: "``foo` stays literal",
			want: "``foo` stays literal",
		},
		{
			name: "minimal single-backtick span terminates",
			text: "`foo`",
			want: "<code>foo</code>",
		},
		{
			name: "trailing lone backtick at end of input",
			text: "see `foo` then a lone `",
			want: "see <code>foo</code> then a lone `",
		},
		{
			name: "input is a single unmatched backtick",
			text: "`",
			want: "`",
		},
		{
			name: "trailing unmatched double-backtick run",
			text: "trailing run ``",
			want: "trailing run ``",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			draft := &render.QuizDraft{
				Title:       "Code",
				MCQuestions: []render.Question{{Number: 1, Text: tt.text, Options: []render.Option{{Text: "ok", IsCorrect: true}}}},
			}

			// Real-time bounding (not testing/synctest) is required here:
			// synctest's fake clock only advances once every goroutine in
			// its bubble is durably blocked, but an infinite loop spinning
			// on arithmetic never blocks, so a synctest timeout would never
			// fire. A real-time deadline is the only idiom that can
			// actually detect non-termination.
			resultCh := make(chan wrapCodeSpansResult, 1)
			go func() {
				assessment, err := qti.BuildAssessment(draft)
				if err != nil {
					resultCh <- wrapCodeSpansResult{err: err}
					return
				}
				xmlBytes, err := qti.Marshal(assessment)
				resultCh <- wrapCodeSpansResult{xml: xmlBytes, err: err}
			}()

			var res wrapCodeSpansResult
			select {
			case res = <-resultCh:
			case <-time.After(2 * time.Second):
				t.Fatalf("wrapCodeSpans did not terminate within 2s for input %q; this indicates the wrapCodeSpans progress invariant is broken", tt.text)
			}

			if res.err != nil {
				t.Fatal(res.err)
			}
			if !strings.Contains(string(res.xml), tt.want) {
				t.Fatalf("marshaled XML = %s, want it to contain %q", res.xml, tt.want)
			}
		})
	}
}
