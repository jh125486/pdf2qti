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
	Material Material `xml:"material"`
	RespDecl RespDecl `xml:"response_lid"`
}

// Material holds text content.
type Material struct {
	MatText string `xml:"mattext"`
}

// RespDecl holds response choices.
type RespDecl struct {
	Ident        string       `xml:"ident,attr"`
	RCardinality string       `xml:"rcardinality,attr"`
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
}

// AndCondition holds multiple varequal conditions joined by logical AND.
type AndCondition struct {
	VarEquals []VarEqual `xml:"varequal"`
}

// VarEqual checks for a specific response.
type VarEqual struct {
	RespIdent string `xml:"respident,attr"`
	Value     string `xml:",chardata"`
}

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
	a.Assessment.Sections = []Section{{Items: items}}
	return a, nil
}

func buildItem(idx int, q render.Question, isMA bool) (Item, error) {
	ident := fmt.Sprintf("q%d", idx)
	respIdent := fmt.Sprintf("%s_resp", ident)
	choices := make([]ResponseLabel, 0, len(q.Options))
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
			RespDecl: RespDecl{
				Ident:        respIdent,
				RCardinality: cardinality,
				Render:       RenderChoice{Choices: choices},
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

// Marshal encodes an Assessment to XML bytes.
func Marshal(a *Assessment) ([]byte, error) {
	out, err := xml.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal QTI: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}
