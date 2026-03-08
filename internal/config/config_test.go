package config_test

import (
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
)

func TestLoad_Valid(t *testing.T) {
	cfg, err := config.Load("testdata/valid.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(cfg.Sources))
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	_, err := config.Load("testdata/no_such_file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidVersion(t *testing.T) {
	_, err := config.Load("testdata/invalid_version.json")
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestLoad_NoSources(t *testing.T) {
	_, err := config.Load("testdata/no_sources.json")
	if err == nil {
		t.Fatal("expected error for empty sources")
	}
}

func TestValidate_VersionNot1(t *testing.T) {
	c := &config.Config{
		Version: 2,
		Sources: []config.Source{{ID: "x", PDF: "x.pdf"}},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for version != 1")
	}
}

func TestValidate_EmptySources(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Sources: []config.Source{},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty sources")
	}
}

func TestValidate_MissingSourceID(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Sources: []config.Source{{PDF: "x.pdf"}},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for missing source ID")
	}
}

func TestValidate_MissingSourcePDF(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Sources: []config.Source{{ID: "x"}},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for missing source PDF")
	}
}

func TestEffectiveWorkflow_Merging(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{
				OutDir:       "default-out",
				OpenReview:   false,
				ReviewTarget: "default-target",
			},
		},
		Sources: []config.Source{
			{
				ID:  "s1",
				PDF: "s1.pdf",
				Workflow: &config.Workflow{
					OutDir:     "source-out",
					OpenReview: true,
				},
			},
		},
	}

	src := &c.Sources[0]
	wf := c.EffectiveWorkflow(src)
	if wf.OutDir != "source-out" {
		t.Errorf("expected OutDir %q, got %q", "source-out", wf.OutDir)
	}
	if !wf.OpenReview {
		t.Error("expected OpenReview true")
	}
	// ReviewTarget not set in source, should keep default
	if wf.ReviewTarget != "default-target" {
		t.Errorf("expected ReviewTarget %q, got %q", "default-target", wf.ReviewTarget)
	}
}

func TestEffectiveWorkflow_NoSourceOverride(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{OutDir: "default-out"},
		},
		Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
	}
	wf := c.EffectiveWorkflow(&c.Sources[0])
	if wf.OutDir != "default-out" {
		t.Errorf("expected %q, got %q", "default-out", wf.OutDir)
	}
}

func TestEffectiveQuiz_SourceOverride(t *testing.T) {
	customQuiz := &config.Quiz{TitleTemplate: "custom", Counts: config.Counts{TF: 5, MA: 5, MC: 5}}
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Quiz: config.Quiz{TitleTemplate: "default"},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Quiz: customQuiz},
		},
	}
	got := c.EffectiveQuiz(&c.Sources[0])
	if got.TitleTemplate != "custom" {
		t.Errorf("expected custom quiz, got %q", got.TitleTemplate)
	}
}

func TestEffectiveQuiz_NoSourceOverride(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Quiz: config.Quiz{TitleTemplate: "default"},
		},
		Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
	}
	got := c.EffectiveQuiz(&c.Sources[0])
	if got.TitleTemplate != "default" {
		t.Errorf("expected default quiz, got %q", got.TitleTemplate)
	}
}

func TestEffectiveGeneration_SourceOverride(t *testing.T) {
	customGen := &config.Generation{Model: "gpt-4o-mini"}
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Generation: config.Generation{Model: "gpt-4o"},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Generation: customGen},
		},
	}
	got := c.EffectiveGeneration(&c.Sources[0])
	if got.Model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %q", got.Model)
	}
}

func TestEffectiveGeneration_NoSourceOverride(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Generation: config.Generation{Model: "gpt-4o"},
		},
		Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
	}
	got := c.EffectiveGeneration(&c.Sources[0])
	if got.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", got.Model)
	}
}

func TestEffectiveValidation_SourceOverride(t *testing.T) {
	customVal := &config.Validation{MAMaxCorrectDensity: 0.3}
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Validation: config.Validation{MAMaxCorrectDensity: 0.5},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Validation: customVal},
		},
	}
	got := c.EffectiveValidation(&c.Sources[0])
	if got.MAMaxCorrectDensity != 0.3 {
		t.Errorf("expected 0.3, got %f", got.MAMaxCorrectDensity)
	}
}

func TestEffectiveValidation_NoSourceOverride(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Validation: config.Validation{MAMaxCorrectDensity: 0.5},
		},
		Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
	}
	got := c.EffectiveValidation(&c.Sources[0])
	if got.MAMaxCorrectDensity != 0.5 {
		t.Errorf("expected 0.5, got %f", got.MAMaxCorrectDensity)
	}
}

func TestOutDir_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.Config
		srcIdx   int
		expected string
	}{
		{
			name: "source outDir wins",
			cfg: config.Config{
				Version: 1,
				Defaults: config.Defaults{
					Workflow: config.Workflow{OutDir: "default-out"},
				},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{OutDir: "src-out"}},
				},
			},
			expected: "src-out",
		},
		{
			name: "defaults outDir",
			cfg: config.Config{
				Version: 1,
				Defaults: config.Defaults{
					Workflow: config.Workflow{OutDir: "default-out"},
				},
				Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			expected: "default-out",
		},
		{
			name: "fallback to out",
			cfg: config.Config{
				Version: 1,
				Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			expected: "out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.OutDir(&tt.cfg.Sources[0])
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
