package generate_test

import (
	"context"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/generate"
)

func TestGenerateStage_TF(t *testing.T) {
	g := generate.New(config.Generation{})
	qs, err := g.GenerateStage(context.Background(), config.StageTF, "some text", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qs) != 3 {
		t.Errorf("expected 3 questions, got %d", len(qs))
	}
	for _, q := range qs {
		correctCount := 0
		for _, o := range q.Options {
			if o.IsCorrect {
				correctCount++
			}
		}
		if correctCount != 1 {
			t.Errorf("TF question should have exactly 1 correct answer, got %d", correctCount)
		}
		if len(q.Options) != 2 {
			t.Errorf("TF question should have 2 options (True/False), got %d", len(q.Options))
		}
	}
}

func TestGenerateStage_MA(t *testing.T) {
	g := generate.New(config.Generation{})
	qs, err := g.GenerateStage(context.Background(), config.StageMA, "some text", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qs) != 2 {
		t.Errorf("expected 2 questions, got %d", len(qs))
	}
	for _, q := range qs {
		if len(q.Options) == 0 {
			t.Error("MA question should have options")
		}
	}
}

func TestGenerateStage_MC(t *testing.T) {
	g := generate.New(config.Generation{})
	qs, err := g.GenerateStage(context.Background(), config.StageMC, "some text", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qs) != 5 {
		t.Errorf("expected 5 questions, got %d", len(qs))
	}
	for _, q := range qs {
		correctCount := 0
		for _, o := range q.Options {
			if o.IsCorrect {
				correctCount++
			}
		}
		if correctCount != 1 {
			t.Errorf("MC question should have exactly 1 correct answer, got %d", correctCount)
		}
	}
}

func TestGenerateStage_Zero(t *testing.T) {
	g := generate.New(config.Generation{})
	qs, err := g.GenerateStage(context.Background(), config.StageTF, "text", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qs) != 0 {
		t.Errorf("expected 0 questions, got %d", len(qs))
	}
}

func TestNew(t *testing.T) {
	cfg := config.Generation{
		Provider: "openai",
		Model:    "gpt-4o",
	}
	g := generate.New(cfg)
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
}
