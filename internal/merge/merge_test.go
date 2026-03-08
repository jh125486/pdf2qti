package merge_test

import (
	"testing"

	"github.com/jh125486/pdf2qti/internal/merge"
	"github.com/jh125486/pdf2qti/internal/render"
)

func makeQuestions(texts ...string) []render.Question {
	qs := make([]render.Question, len(texts))
	for i, t := range texts {
		qs[i] = render.Question{Number: i + 1, Text: t}
	}
	return qs
}

func TestMerge_Basic(t *testing.T) {
	tf := makeQuestions("TF1", "TF2")
	ma := makeQuestions("MA1")
	mc := makeQuestions("MC1", "MC2", "MC3")

	all := merge.Merge(tf, ma, mc)
	if len(all) != 6 {
		t.Fatalf("expected 6 questions, got %d", len(all))
	}
	// Check sequential numbering
	for i, q := range all {
		if q.Number != i+1 {
			t.Errorf("question[%d].Number = %d, want %d", i, q.Number, i+1)
		}
	}
	// Check order
	if all[0].Text != "TF1" {
		t.Errorf("expected TF1 first, got %q", all[0].Text)
	}
	if all[2].Text != "MA1" {
		t.Errorf("expected MA1 at index 2, got %q", all[2].Text)
	}
	if all[3].Text != "MC1" {
		t.Errorf("expected MC1 at index 3, got %q", all[3].Text)
	}
}

func TestMerge_Empty(t *testing.T) {
	all := merge.Merge(nil, nil, nil)
	if len(all) != 0 {
		t.Errorf("expected empty, got %d", len(all))
	}
}

func TestMerge_OnlyTF(t *testing.T) {
	tf := makeQuestions("A", "B")
	all := merge.Merge(tf, nil, nil)
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	for i, q := range all {
		if q.Number != i+1 {
			t.Errorf("wrong number at %d: %d", i, q.Number)
		}
	}
}

func TestSplitDraft_Basic(t *testing.T) {
	qs := makeQuestions("TF1", "TF2", "MA1", "MC1", "MC2")
	tf, ma, mc := merge.SplitDraft(qs, 2, 1, 2)

	if len(tf) != 2 {
		t.Errorf("expected 2 TF, got %d", len(tf))
	}
	if len(ma) != 1 {
		t.Errorf("expected 1 MA, got %d", len(ma))
	}
	if len(mc) != 2 {
		t.Errorf("expected 2 MC, got %d", len(mc))
	}

	// Check per-section numbering
	for i, q := range tf {
		if q.Number != i+1 {
			t.Errorf("tf[%d].Number = %d, want %d", i, q.Number, i+1)
		}
	}
	for i, q := range mc {
		if q.Number != i+1 {
			t.Errorf("mc[%d].Number = %d, want %d", i, q.Number, i+1)
		}
	}
}

func TestSplitDraft_FewerThanRequested(t *testing.T) {
	qs := makeQuestions("Q1", "Q2")
	tf, ma, mc := merge.SplitDraft(qs, 5, 5, 5)
	if len(tf)+len(ma)+len(mc) != 2 {
		t.Errorf("expected 2 total, got %d", len(tf)+len(ma)+len(mc))
	}
}

func TestSplitDraft_Empty(t *testing.T) {
	tf, ma, mc := merge.SplitDraft(nil, 1, 1, 1)
	if len(tf) != 0 || len(ma) != 0 || len(mc) != 0 {
		t.Error("expected all empty")
	}
}
