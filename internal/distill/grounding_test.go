package distill_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

type groundingLLM struct {
	synthesis string
	evidence  func(prompt string) string
	decision  func(prompt string) string
}

func (m *groundingLLM) Complete(_ context.Context, prompt string, _ *distill.Schema) (string, error) {
	switch {
	case strings.HasPrefix(prompt, "GROUNDING_EVIDENCE:"):
		return m.evidence(prompt), nil
	case strings.HasPrefix(prompt, "GROUNDING_DECISION:"):
		return m.decision(prompt), nil
	default:
		return m.synthesis, nil
	}
}

func TestVerifyGrounding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		source      string
		evidence    string
		decision    string
		wantError   string
		wantFinding string
	}{
		{name: "verified quote supports", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":["Signals use SIGINT."]}]}`, decision: `{"items":[{"id":"/overview","supported":true,"reason":""}]}`},
		{name: "whitespace normalized quote", source: "Signals\nuse  SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":["Signals use SIGINT."]}]}`, decision: `{"items":[{"id":"/overview","supported":true,"reason":""}]}`},
		{name: "fenced JSON replies repaired", source: "Signals use SIGINT.", evidence: "```json\n{\"items\":[{\"id\":\"/overview\",\"evidence_quotes\":[]}]}\n```", decision: "Here you go: {\"items\":[{\"id\":\"/overview\",\"supported\":false,\"reason\":\"No source evidence for SIGHUP\"}]}", wantFinding: "/overview: No source evidence for SIGHUP"},
		{name: "unsupported claim", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":[]}]}`, decision: `{"items":[{"id":"/overview","supported":false,"reason":"No source evidence for SIGHUP"}]}`, wantFinding: "/overview: No source evidence for SIGHUP"},
		{name: "invented quote", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":["SIGHUP is ignored."]}]}`, wantError: "quote is empty or absent"},
		{name: "missing ID", source: "Signals use SIGINT.", evidence: `{"items":[]}`, wantError: `missing item ID "/overview"`},
		{name: "duplicate ID", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":[]},{"id":"/overview","evidence_quotes":[]}]}`, wantError: `duplicate item ID "/overview"`},
		{name: "unknown ID", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/other","evidence_quotes":[]}]}`, wantError: `unknown item ID "/other"`},
		{name: "missing evidence array", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview"}]}`, wantError: "missing evidence_quotes array"},
		{name: "unsupported without verified quote", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":[]}]}`, decision: `{"items":[{"id":"/overview","supported":true,"reason":""}]}`, wantError: "supported without verified evidence"},
		{name: "missing decision ID", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":["Signals use SIGINT."]}]}`, decision: `{"items":[]}`, wantError: `missing item ID "/overview"`},
		{name: "missing decision support", source: "Signals use SIGINT.", evidence: `{"items":[{"id":"/overview","evidence_quotes":["Signals use SIGINT."]}]}`, decision: `{"items":[{"id":"/overview"}]}`, wantError: "missing supported boolean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &groundingLLM{evidence: func(_ string) string { return tt.evidence }, decision: func(_ string) string { return tt.decision }}
			findings, err := distill.VerifyGrounding(t.Context(), model, &distill.DistilledContext{Overview: "Signals use SIGHUP."}, tt.source)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error=%v want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if len(findings) > 0 {
				got = findings[0].ID + ": " + findings[0].Reason
			}
			if got != tt.wantFinding {
				t.Fatalf("finding=%q want=%q", got, tt.wantFinding)
			}
		})
	}
}

func TestVerifyGroundingAcrossChunks(t *testing.T) {
	t.Parallel()
	model := &groundingLLM{
		evidence: func(prompt string) string {
			if strings.Contains(prompt, "ORIGINAL SOURCE CHUNK:\nfact") {
				return `{"items":[{"id":"/overview","evidence_quotes":["fact"]}]}`
			}
			return `{"items":[{"id":"/overview","evidence_quotes":[]}]}`
		},
		decision: func(_ string) string { return `{"items":[{"id":"/overview","supported":true,"reason":""}]}` },
	}
	source := strings.Repeat("a", 45000) + "\n\nfact"
	findings, err := distill.VerifyGrounding(t.Context(), model, &distill.DistilledContext{Overview: "fact"}, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %v", findings)
	}
}

func TestVerifyGroundingTeachingAdvice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, notes, decision, wantError, wantFinding string
	}{
		{name: "recommendation only", notes: "Start with a diagram.", decision: `{"items":[{"id":"/teaching_notes","supported":true,"pedagogical_only":true,"reason":""}]}`},
		{name: "mixed claim cannot bypass evidence", notes: "Start with a diagram. Fork duplicates threads.", decision: `{"items":[{"id":"/teaching_notes","supported":true,"pedagogical_only":false,"reason":""}]}`, wantError: "supported without verified evidence"},
		{name: "mixed claim reported", notes: "Start with a diagram. Fork duplicates threads.", decision: `{"items":[{"id":"/teaching_notes","supported":false,"pedagogical_only":false,"reason":"fork claim absent"}]}`, wantFinding: "/teaching_notes: fork claim absent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &groundingLLM{
				evidence: func(_ string) string { return `{"items":[{"id":"/teaching_notes","evidence_quotes":[]}]}` },
				decision: func(_ string) string { return tt.decision },
			}
			findings, err := distill.VerifyGrounding(t.Context(), model, &distill.DistilledContext{TeachingNotes: tt.notes}, "source facts")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error=%v want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if len(findings) > 0 {
				got = findings[0].ID + ": " + findings[0].Reason
			}
			if got != tt.wantFinding {
				t.Fatalf("finding=%q want=%q", got, tt.wantFinding)
			}
		})
	}
}

func TestVerifyGroundingLinkedVocabulary(t *testing.T) {
	t.Parallel()
	var sawLinkedCandidate bool
	model := &groundingLLM{
		evidence: func(prompt string) string {
			sawLinkedCandidate = strings.Contains(prompt, `"content":"Term: Alpha\nDefinition: second"`)
			return `{"items":[{"id":"/vocabulary/0","evidence_quotes":["Alpha means first.","Beta means second."]}]}`
		},
		decision: func(_ string) string {
			return `{"items":[{"id":"/vocabulary/0","supported":false,"reason":"definition belongs to Beta"}]}`
		},
	}
	findings, err := distill.VerifyGrounding(t.Context(), model, &distill.DistilledContext{Vocabulary: []distill.VocabTerm{{Term: "Alpha", Definition: "second"}}}, "Alpha means first. Beta means second.")
	if err != nil {
		t.Fatal(err)
	}
	if !sawLinkedCandidate {
		t.Fatal("term and definition not checked as one candidate")
	}
	if len(findings) != 1 || findings[0].ID != "/vocabulary/0" {
		t.Fatalf("findings=%v", findings)
	}
}

func TestDistillGroundingPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode         string
		wantError    bool
		wantOverview string
		wantWarning  bool
	}{
		{mode: "fail", wantError: true},
		{mode: "drop", wantOverview: "", wantWarning: true},
		{mode: "warn", wantOverview: "unsupported fact", wantWarning: true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			synthesis, _ := json.Marshal(distill.DistilledContext{Overview: "unsupported fact"})
			model := &groundingLLM{
				synthesis: string(synthesis),
				evidence:  func(_ string) string { return `{"items":[{"id":"/overview","evidence_quotes":[]}]}` },
				decision: func(_ string) string {
					return `{"items":[{"id":"/overview","supported":false,"reason":"No matching source fact"}]}`
				},
			}
			dc, err := distill.Distill(t.Context(), &config.Source{ID: "s", PDF: "s.pdf"}, nil, model, "original fact", tt.mode)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "/overview: No matching source fact") {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dc.Overview != tt.wantOverview {
				t.Fatalf("overview=%q want=%q", dc.Overview, tt.wantOverview)
			}
			if (len(dc.VerificationWarnings) > 0) != tt.wantWarning {
				t.Fatalf("warnings=%v", dc.VerificationWarnings)
			}
		})
	}
}

func TestVerifyGroundingFieldCoverage(t *testing.T) {
	t.Parallel()
	dc := &distill.DistilledContext{
		SourceID: "config-id", Book: "config-book", Chapter: 3,
		ModuleName: "fact", Text: "fact", Overview: "fact", KeyConcepts: []string{"fact"},
		MaterialOverview: "fact", TeachingNotes: "fact",
		Objectives: []distill.Objective{{CO: 1, Text: "fact"}},
		Vocabulary: []distill.VocabTerm{{Term: "fact", Definition: "fact"}},
		Theorems:   []distill.Theorem{{Name: "fact", Statement: "fact"}},
		Sections:   []distill.Section{{Title: "fact", Summary: "fact"}},
		Agenda:     []string{"fact"}, Slides: []distill.Slide{{Title: "fact", Content: "fact", Tag: "fact", Diagram: &distill.Diagram{Source: "fact", Alt: "fact", Caption: "fact"}}},
	}
	var seen []string
	model := &groundingLLM{
		evidence: func(prompt string) string {
			ids := groundingPromptIDs(prompt)
			seen = append(seen, ids...)
			items := make([]map[string]any, len(ids))
			for i, id := range ids {
				items[i] = map[string]any{"id": id, "evidence_quotes": []string{"fact"}}
			}
			b, _ := json.Marshal(map[string]any{"items": items})
			return string(b)
		},
		decision: func(prompt string) string {
			ids := groundingPromptIDs(prompt)
			items := make([]map[string]any, len(ids))
			for i, id := range ids {
				items[i] = map[string]any{"id": id, "supported": true, "reason": ""}
			}
			b, _ := json.Marshal(map[string]any{"items": items})
			return string(b)
		},
	}
	findings, err := distill.VerifyGrounding(t.Context(), model, dc, "fact")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v", findings)
	}
	slices.Sort(seen)
	want := []string{"/agenda/0", "/key_concepts/0", "/material_overview", "/module_name", "/objectives/0", "/overview", "/sections/0", "/slides/0/content", "/slides/0/diagram/alt", "/slides/0/diagram/caption", "/slides/0/diagram/source", "/slides/0/tag", "/slides/0/title", "/teaching_notes", "/text", "/theorems/0", "/vocabulary/0"}
	slices.Sort(want)
	if !slices.Equal(seen, want) {
		t.Fatalf("IDs=%v want=%v", seen, want)
	}
}

func groundingPromptIDs(prompt string) []string {
	var raw string
	if strings.HasPrefix(prompt, "GROUNDING_EVIDENCE:") {
		raw = strings.SplitN(strings.SplitN(prompt, "CANDIDATES:\n", 2)[1], "\nORIGINAL SOURCE CHUNK:", 2)[0]
	} else {
		raw = strings.SplitN(prompt, "CANDIDATES AND VERIFIED EVIDENCE:\n", 2)[1]
	}
	var items []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(raw), &items)
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func TestDropUngrounded(t *testing.T) {
	t.Parallel()
	dc := &distill.DistilledContext{
		Overview: "unsupported", KeyConcepts: []string{"keep", "drop"},
		Objectives: []distill.Objective{{CO: 1, Text: "drop"}, {CO: 2, Text: "keep"}},
		Vocabulary: []distill.VocabTerm{{Term: "drop", Definition: "drop"}, {Term: "keep", Definition: "keep"}},
		Theorems:   []distill.Theorem{{Name: "drop", Statement: "drop"}, {Name: "keep", Statement: "keep"}},
		Sections:   []distill.Section{{Title: "drop", Summary: "drop"}, {Title: "keep", Summary: "keep"}},
		Slides:     []distill.Slide{{Tag: "drop"}},
	}
	distill.DropUngrounded(dc, []distill.GroundingFinding{{ID: "/overview"}, {ID: "/key_concepts/1"}, {ID: "/objectives/0"}, {ID: "/vocabulary/0"}, {ID: "/theorems/0"}, {ID: "/sections/0"}, {ID: "/slides/0/tag"}})
	if dc.Overview != "" || !slices.Equal(dc.KeyConcepts, []string{"keep"}) || len(dc.Objectives) != 1 || dc.Objectives[0].CO != 2 || len(dc.Vocabulary) != 1 || dc.Vocabulary[0].Term != "keep" || len(dc.Theorems) != 1 || dc.Theorems[0].Name != "keep" || len(dc.Sections) != 1 || dc.Sections[0].Title != "keep" || dc.Slides[0].Tag != "" {
		t.Fatalf("drop result=%+v", dc)
	}
}
