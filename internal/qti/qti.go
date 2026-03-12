// Package qti provides QTI 1.2 XML generation for Canvas LMS.
package qti

import (
	"encoding/xml"
	"fmt"

	"github.com/jh125486/pdf2qti/internal/render"
)

// Assessment represents a QTI assessment.
type Assessment struct {
	XMLName    xml.Name   `xml:"questestinterop"`
	Assessment innerAssmt `xml:"assessment"`
}

type innerAssmt struct {
	Title    string    `xml:"title,attr"`
	Sections []Section `xml:"section"`
}

// Section represents a QTI section.
type Section struct {
	Items []Item `xml:"item"`
}

// Item represents a QTI item.
type Item struct {
	Ident    string   `xml:"ident,attr"`
	Title    string   `xml:"title,attr"`
	ItemBody ItemBody `xml:"presentation"`
	ResForm  ResForm  `xml:"resprocessing"`
}

// ItemBody holds the question text and responses.
type ItemBody struct {
	Material  Material   `xml:"material"`
	RespDecls []RespDecl `xml:"response_lid,omitempty"`
	RespStr   *RespStr   `xml:"response_str,omitempty"`
	RespNum   *RespNum   `xml:"response_num,omitempty"`
}

// Material holds text content.
type Material struct {
	MatText string `xml:"mattext"`
}

// RespDecl holds response choices.
type RespDecl struct {
	Ident        string       `xml:"ident,attr"`
	RCardinality string       `xml:"rcardinality,attr"`
	Material     *Material    `xml:"material,omitempty"`
	Render       RenderChoice `xml:"render_choice"`
}

// RenderChoice holds answer choices.
type RenderChoice struct {
	Choices []ResponseLabel `xml:"response_label"`
}

// ResponseLabel is a single answer choice.
type ResponseLabel struct {
	Ident    string   `xml:"ident,attr"`
	Material Material `xml:"material"`
}

// RespStr holds a string response (used for short answer and essay).
type RespStr struct {
	Ident        string    `xml:"ident,attr"`
	RCardinality string    `xml:"rcardinality,attr"`
	Render       RenderFib `xml:"render_fib"`
}

// RespNum holds a numeric response.
type RespNum struct {
	Ident        string    `xml:"ident,attr"`
	RCardinality string    `xml:"rcardinality,attr"`
	Render       RenderFib `xml:"render_fib"`
}

// RenderFib holds a fill-in-the-blank renderer.
type RenderFib struct {
	Rows    int    `xml:"rows,attr,omitempty"`
	Columns int    `xml:"columns,attr,omitempty"`
	Prompt  string `xml:"prompt,attr,omitempty"`
	FibType string `xml:"fibtype,attr,omitempty"`
}

// ResForm holds scoring logic.
type ResForm struct {
	Outcomes     Outcomes       `xml:"outcomes"`
	ResCondition []ResCondition `xml:"respcondition"`
}

// Outcomes holds score variables.
type Outcomes struct {
	Decvar Decvar `xml:"decvar"`
}

// Decvar declares a score variable.
type Decvar struct {
	MaxValue string `xml:"maxvalue,attr"`
	MinValue string `xml:"minvalue,attr"`
	VarName  string `xml:"varname,attr"`
	VarType  string `xml:"vartype,attr"`
}

// ResCondition defines a scoring condition.
type ResCondition struct {
	Continue     string       `xml:"continue,attr"`
	ConditionVar ConditionVar `xml:"conditionvar"`
	SetVar       SetVar       `xml:"setvar"`
}

// ConditionVar holds a condition.
type ConditionVar struct {
	And      *AndCondition `xml:"and,omitempty"`
	VarEqual *VarEqual     `xml:"varequal,omitempty"`
	VarGTE   *VarCompare   `xml:"vargte,omitempty"`
	VarLTE   *VarCompare   `xml:"varlte,omitempty"`
	Other    *OtherCond    `xml:"other,omitempty"`
}

// AndCondition holds multiple varequal conditions joined by logical AND.
type AndCondition struct {
	VarEquals []VarEqual `xml:"varequal"`
}

// VarEqual checks for a specific response.
type VarEqual struct {
	RespIdent string `xml:"respident,attr"`
	Case      string `xml:"case,attr,omitempty"`
	Value     string `xml:",chardata"`
}

// VarCompare checks for a numeric comparison (>=  or <=).
type VarCompare struct {
	RespIdent string `xml:"respident,attr"`
	Value     string `xml:",chardata"`
}

// OtherCond matches any response (used for essay questions).
type OtherCond struct{}

// SetVar sets a score variable.
type SetVar struct {
	Action  string `xml:"action,attr"`
	VarName string `xml:"varname,attr"`
	Value   string `xml:",chardata"`
}

// BuildAssessment converts a QuizDraft to a QTI Assessment.
func BuildAssessment(d *render.QuizDraft) (*Assessment, error) {
	if d.Title == "" {
		return nil, fmt.Errorf("quiz draft must have a title")
	}
	a := &Assessment{
		Assessment: innerAssmt{
			Title: d.Title,
		},
	}
	var items []Item
	offset := 0
	for i, q := range d.TFQuestions {
		item, err := buildItem(offset+i+1, q, false)
		if err != nil {
			return nil, fmt.Errorf("build TF item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	offset += len(d.TFQuestions)
	for i, q := range d.MAQuestions {
		item, err := buildItem(offset+i+1, q, true)
		if err != nil {
			return nil, fmt.Errorf("build MA item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	offset += len(d.MAQuestions)
	for i, q := range d.MCQuestions {
		item, err := buildItem(offset+i+1, q, false)
		if err != nil {
			return nil, fmt.Errorf("build MC item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	offset += len(d.MCQuestions)
	for i, q := range d.SAQuestions {
		item, err := buildSAItem(offset+i+1, q)
		if err != nil {
			return nil, fmt.Errorf("build SA item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	offset += len(d.SAQuestions)
	for i, q := range d.ESQuestions {
		item := buildESItem(offset+i+1, q)
		items = append(items, item)
	}
	offset += len(d.ESQuestions)
	for i, q := range d.MTQuestions {
		item, err := buildMTItem(offset+i+1, q)
		if err != nil {
			return nil, fmt.Errorf("build MT item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	offset += len(d.MTQuestions)
	for i, q := range d.NRQuestions {
		item, err := buildNRItem(offset+i+1, q)
		if err != nil {
			return nil, fmt.Errorf("build NR item %d: %w", offset+i+1, err)
		}
		items = append(items, item)
	}
	a.Assessment.Sections = []Section{{Items: items}}
	return a, nil
}

func buildItem(idx int, q render.Question, isMA bool) (Item, error) {
	ident := fmt.Sprintf("q%d", idx)
	respIdent := fmt.Sprintf("%s_resp", ident)
	var choices []ResponseLabel
	var correctIdents []string
	for j, o := range q.Options {
		choiceID := fmt.Sprintf("%s_c%d", ident, j+1)
		choices = append(choices, ResponseLabel{
			Ident:    choiceID,
			Material: Material{MatText: o.Text},
		})
		if o.IsCorrect {
			correctIdents = append(correctIdents, choiceID)
		}
	}
	if len(correctIdents) == 0 {
		return Item{}, fmt.Errorf("question %d has no correct option", idx)
	}

	cardinality := "Single"
	if isMA {
		cardinality = "Multiple"
	}

	var conditionVar ConditionVar
	if isMA && len(correctIdents) > 1 {
		varEquals := make([]VarEqual, len(correctIdents))
		for k, id := range correctIdents {
			varEquals[k] = VarEqual{RespIdent: respIdent, Value: id}
		}
		conditionVar = ConditionVar{And: &AndCondition{VarEquals: varEquals}}
	} else {
		conditionVar = ConditionVar{
			VarEqual: &VarEqual{RespIdent: respIdent, Value: correctIdents[0]},
		}
	}

	item := Item{
		Ident: ident,
		Title: fmt.Sprintf("Question %d", idx),
		ItemBody: ItemBody{
			Material: Material{MatText: q.Text},
			RespDecls: []RespDecl{
				{
					Ident:        respIdent,
					RCardinality: cardinality,
					Render:       RenderChoice{Choices: choices},
				},
			},
		},
		ResForm: ResForm{
			Outcomes: Outcomes{
				Decvar: Decvar{
					MaxValue: "100",
					MinValue: "0",
					VarName:  "SCORE",
					VarType:  "Decimal",
				},
			},
			ResCondition: []ResCondition{
				{
					Continue:     "No",
					ConditionVar: conditionVar,
					SetVar: SetVar{
						Action:  "Set",
						VarName: "SCORE",
						Value:   "100",
					},
				},
			},
		},
	}
	return item, nil
}

// buildSAItem builds a QTI item for a Short Answer question.
func buildSAItem(idx int, q render.Question) (Item, error) {
	if len(q.Options) == 0 {
		return Item{}, fmt.Errorf("short answer question %d has no accepted answers", idx)
	}
	ident := fmt.Sprintf("q%d", idx)
	respIdent := fmt.Sprintf("%s_resp", ident)

	// Build one respcondition per accepted answer (case-insensitive).
	var conditions []ResCondition
	for _, o := range q.Options {
		conditions = append(conditions, ResCondition{
			Continue: "No",
			ConditionVar: ConditionVar{
				VarEqual: &VarEqual{RespIdent: respIdent, Case: "No", Value: o.Text},
			},
			SetVar: SetVar{Action: "Set", VarName: "SCORE", Value: "100"},
		})
	}

	return Item{
		Ident: ident,
		Title: fmt.Sprintf("Question %d", idx),
		ItemBody: ItemBody{
			Material: Material{MatText: q.Text},
			RespStr: &RespStr{
				Ident:        respIdent,
				RCardinality: "Single",
				Render:       RenderFib{Rows: 1, Columns: 20},
			},
		},
		ResForm: ResForm{
			Outcomes: Outcomes{
				Decvar: Decvar{MaxValue: "100", MinValue: "0", VarName: "SCORE", VarType: "Decimal"},
			},
			ResCondition: conditions,
		},
	}, nil
}

// buildESItem builds a QTI item for an Essay question.
func buildESItem(idx int, q render.Question) Item {
	ident := fmt.Sprintf("q%d", idx)
	respIdent := fmt.Sprintf("%s_resp", ident)
	return Item{
		Ident: ident,
		Title: fmt.Sprintf("Question %d", idx),
		ItemBody: ItemBody{
			Material: Material{MatText: q.Text},
			RespStr: &RespStr{
				Ident:        respIdent,
				RCardinality: "Single",
				Render:       RenderFib{Rows: 10, Columns: 80, Prompt: "Box"},
			},
		},
		ResForm: ResForm{
			Outcomes: Outcomes{
				Decvar: Decvar{MaxValue: "100", MinValue: "0", VarName: "SCORE", VarType: "Decimal"},
			},
			ResCondition: []ResCondition{
				{
					Continue:     "No",
					ConditionVar: ConditionVar{Other: &OtherCond{}},
					SetVar:       SetVar{Action: "Set", VarName: "SCORE", Value: "0"},
				},
			},
		},
	}
}

// buildMTItem builds a QTI item for a Matching question.
// Each left-side item becomes a response_lid; all right-side items are choices.
func buildMTItem(idx int, q render.Question) (Item, error) {
	if len(q.Options) == 0 {
		return Item{}, fmt.Errorf("matching question %d has no pairs", idx)
	}
	ident := fmt.Sprintf("q%d", idx)

	// Collect all right-side answer labels.
	rightLabels := make([]ResponseLabel, len(q.Options))
	for j, o := range q.Options {
		rightLabels[j] = ResponseLabel{
			Ident:    fmt.Sprintf("%s_match_%d", ident, j+1),
			Material: Material{MatText: o.MatchText},
		}
	}

	// Each left-side item gets its own response_lid with all right-side labels as choices.
	respDecls := make([]RespDecl, len(q.Options))
	for j, o := range q.Options {
		respDecls[j] = RespDecl{
			Ident:        fmt.Sprintf("%s_resp_%d", ident, j+1),
			RCardinality: "Single",
			Material:     &Material{MatText: o.Text},
			Render:       RenderChoice{Choices: rightLabels},
		}
	}

	// Build one respcondition per pair; each adds an equal share of score.
	scorePerPair := 100 / len(q.Options)
	conditions := make([]ResCondition, len(q.Options))
	for j := range q.Options {
		conditions[j] = ResCondition{
			Continue: "Yes",
			ConditionVar: ConditionVar{
				VarEqual: &VarEqual{
					RespIdent: fmt.Sprintf("%s_resp_%d", ident, j+1),
					Value:     fmt.Sprintf("%s_match_%d", ident, j+1),
				},
			},
			SetVar: SetVar{Action: "Add", VarName: "SCORE", Value: fmt.Sprintf("%d", scorePerPair)},
		}
	}

	return Item{
		Ident: ident,
		Title: fmt.Sprintf("Question %d", idx),
		ItemBody: ItemBody{
			Material:  Material{MatText: q.Text},
			RespDecls: respDecls,
		},
		ResForm: ResForm{
			Outcomes: Outcomes{
				Decvar: Decvar{MaxValue: "100", MinValue: "0", VarName: "SCORE", VarType: "Decimal"},
			},
			ResCondition: conditions,
		},
	}, nil
}

// buildNRItem builds a QTI item for a Numerical question.
// The first option with IsCorrect=true is the answer value; the first with IsCorrect=false is the tolerance.
func buildNRItem(idx int, q render.Question) (Item, error) {
	if len(q.Options) == 0 {
		return Item{}, fmt.Errorf("numerical question %d has no answer", idx)
	}
	ident := fmt.Sprintf("q%d", idx)
	respIdent := fmt.Sprintf("%s_resp", ident)

	var answerVal string
	var toleranceVal string
	for _, o := range q.Options {
		if o.IsCorrect && answerVal == "" {
			answerVal = o.Text
		} else if !o.IsCorrect && toleranceVal == "" {
			toleranceVal = o.Text
		}
	}
	if answerVal == "" {
		return Item{}, fmt.Errorf("numerical question %d has no answer value", idx)
	}

	var conditionVar ConditionVar
	if toleranceVal != "" {
		// Range condition: answer - tolerance <= response <= answer + tolerance
		var answerFloat, toleranceFloat float64
		if _, err := fmt.Sscanf(answerVal, "%f", &answerFloat); err != nil {
			return Item{}, fmt.Errorf("numerical question %d: invalid answer value %q: %w", idx, answerVal, err)
		}
		if _, err := fmt.Sscanf(toleranceVal, "%f", &toleranceFloat); err != nil {
			return Item{}, fmt.Errorf("numerical question %d: invalid tolerance value %q: %w", idx, toleranceVal, err)
		}
		lo := fmt.Sprintf("%g", answerFloat-toleranceFloat)
		hi := fmt.Sprintf("%g", answerFloat+toleranceFloat)
		conditionVar = ConditionVar{
			VarGTE: &VarCompare{RespIdent: respIdent, Value: lo},
			VarLTE: &VarCompare{RespIdent: respIdent, Value: hi},
		}
	} else {
		conditionVar = ConditionVar{
			VarEqual: &VarEqual{RespIdent: respIdent, Value: answerVal},
		}
	}

	return Item{
		Ident: ident,
		Title: fmt.Sprintf("Question %d", idx),
		ItemBody: ItemBody{
			Material: Material{MatText: q.Text},
			RespNum: &RespNum{
				Ident:        respIdent,
				RCardinality: "Single",
				Render:       RenderFib{FibType: "Decimal"},
			},
		},
		ResForm: ResForm{
			Outcomes: Outcomes{
				Decvar: Decvar{MaxValue: "100", MinValue: "0", VarName: "SCORE", VarType: "Decimal"},
			},
			ResCondition: []ResCondition{
				{
					Continue:     "No",
					ConditionVar: conditionVar,
					SetVar:       SetVar{Action: "Set", VarName: "SCORE", Value: "100"},
				},
			},
		},
	}, nil
}

// Marshal encodes an Assessment to XML bytes.
func Marshal(a *Assessment) ([]byte, error) {
	out, err := xml.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal QTI: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}
