package generate_test

import (
	"context"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/generate"
	"github.com/jh125486/pdf2qti/internal/render"
)

func TestGenerateStage_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stage     config.Stage
		count     int
		wantCount int
		check     func(t *testing.T, qs []render.Question)
	}{
		{name: "tf", stage: config.StageTF, count: 3, wantCount: 3},
		{name: "ma", stage: config.StageMA, count: 2, wantCount: 2},
		{name: "mc", stage: config.StageMC, count: 5, wantCount: 5},
		{name: "sa", stage: config.StageSA, count: 2, wantCount: 2},
		{name: "es", stage: config.StageES, count: 2, wantCount: 2},
		{name: "mt", stage: config.StageMT, count: 2, wantCount: 2},
		{name: "nr", stage: config.StageNR, count: 2, wantCount: 2},
		{name: "zero", stage: config.StageTF, count: 0, wantCount: 0},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := generate.New(config.Generation{})
			qs, err := g.GenerateStage(context.Background(), tt.stage, "some text", tt.count)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(qs) != tt.wantCount {
				t.Fatalf("len=%d want=%d", len(qs), tt.wantCount)
			}
			for _, q := range qs {
				switch tt.stage {
				case config.StageTF, config.StageMC:
					correct := 0
					for _, o := range q.Options {
						if o.IsCorrect {
							correct++
						}
					}
					if correct != 1 {
						t.Fatalf("expected exactly one correct option, got %d", correct)
					}
				case config.StageES:
					if len(q.Options) != 0 {
						t.Fatalf("essay should have no options, got %d", len(q.Options))
					}
				case config.StageMT:
					for _, o := range q.Options {
						if o.MatchText == "" {
							t.Fatal("matching option missing MatchText")
						}
					}
				}
			}
		})
	}
}

func TestNew_ReturnsGenerator(t *testing.T) {
	t.Parallel()
	g := generate.New(config.Generation{Provider: "openai", Model: "gpt-4o"})
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
}
