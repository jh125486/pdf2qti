package merge_test

import (
	"testing"

	"github.com/jh125486/pdf2qti/internal/merge"
	"github.com/jh125486/pdf2qti/internal/render"
)

func makeQuestions(texts ...string) []render.Question {
	qs := make([]render.Question, len(texts))
	for i, txt := range texts {
		qs[i] = render.Question{Number: i + 1, Text: txt}
	}
	return qs
}

func TestMerge_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tf       []render.Question
		ma       []render.Question
		mc       []render.Question
		wantLen  int
		wantText []string
	}{
		{name: "basic", tf: makeQuestions("TF1", "TF2"), ma: makeQuestions("MA1"), mc: makeQuestions("MC1", "MC2", "MC3"), wantLen: 6, wantText: []string{"TF1", "MA1", "MC1"}},
		{name: "empty", wantLen: 0},
		{name: "only tf", tf: makeQuestions("A", "B"), wantLen: 2, wantText: []string{"A"}},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			all := merge.Merge(tt.tf, tt.ma, tt.mc)
			if len(all) != tt.wantLen {
				t.Fatalf("len=%d want=%d", len(all), tt.wantLen)
			}
			for i, q := range all {
				if q.Number != i+1 {
					t.Fatalf("question %d number=%d want=%d", i, q.Number, i+1)
				}
			}
			for _, wt := range tt.wantText {
				found := false
				for _, q := range all {
					if q.Text == wt {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected text %q in merged output", wt)
				}
			}
		})
	}
}

func TestSplitDraft_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		all           []render.Question
		tfCount       int
		maCount       int
		mcCount       int
		wantTF        int
		wantMA        int
		wantMC        int
		wantTotal     int
		checkNumbered bool
	}{
		{name: "basic", all: makeQuestions("TF1", "TF2", "MA1", "MC1", "MC2"), tfCount: 2, maCount: 1, mcCount: 2, wantTF: 2, wantMA: 1, wantMC: 2, wantTotal: 5, checkNumbered: true},
		{name: "fewer than requested", all: makeQuestions("Q1", "Q2"), tfCount: 5, maCount: 5, mcCount: 5, wantTotal: 2, wantTF: 2},
		{name: "empty", tfCount: 1, maCount: 1, mcCount: 1, wantTotal: 0},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tf, ma, mc := merge.SplitDraft(tt.all, tt.tfCount, tt.maCount, tt.mcCount)
			if len(tf) != tt.wantTF || len(ma) != tt.wantMA || len(mc) != tt.wantMC {
				t.Fatalf("lens tf=%d ma=%d mc=%d want tf=%d ma=%d mc=%d", len(tf), len(ma), len(mc), tt.wantTF, tt.wantMA, tt.wantMC)
			}
			if len(tf)+len(ma)+len(mc) != tt.wantTotal {
				t.Fatalf("total=%d want=%d", len(tf)+len(ma)+len(mc), tt.wantTotal)
			}
			if tt.checkNumbered {
				for i, q := range tf {
					if q.Number != i+1 {
						t.Fatalf("tf[%d].Number=%d want=%d", i, q.Number, i+1)
					}
				}
				for i, q := range mc {
					if q.Number != i+1 {
						t.Fatalf("mc[%d].Number=%d want=%d", i, q.Number, i+1)
					}
				}
			}
		})
	}
}
