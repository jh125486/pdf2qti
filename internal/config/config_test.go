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

func TestLoad_BadJSON(t *testing.T) {
	_, err := config.Load("testdata/bad_json.json")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestEffectiveWorkflow_ReviewTargetOverride(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{ReviewTarget: "default-target"},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{ReviewTarget: "source-target"}},
		},
	}
	wf := c.EffectiveWorkflow(&c.Sources[0])
	if wf.ReviewTarget != "source-target" {
		t.Errorf("expected source ReviewTarget %q, got %q", "source-target", wf.ReviewTarget)
	}
}

func TestEffectiveQuiz_AllFields(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Quiz: config.Quiz{
				TitleTemplate:       "default-title",
				DescriptionTemplate: "default-desc",
				Counts:              config.Counts{TF: 1, MA: 1, MC: 1},
				MCOptions:           config.OptionRange{Min: 3, Max: 5},
				MAOptions:           config.OptionRange{Min: 3, Max: 5},
			},
		},
		Sources: []config.Source{
			{
				ID:  "s1",
				PDF: "s1.pdf",
				Quiz: &config.Quiz{
					DescriptionTemplate: "src-desc",
					Counts:              config.Counts{SA: 2, ES: 1, MT: 1, NR: 1},
					MCOptions:           config.OptionRange{Min: 4, Max: 4},
					MAOptions:           config.OptionRange{Min: 4, Max: 6},
				},
			},
		},
	}
	got := c.EffectiveQuiz(&c.Sources[0])
	if got.DescriptionTemplate != "src-desc" {
		t.Errorf("expected DescriptionTemplate %q, got %q", "src-desc", got.DescriptionTemplate)
	}
	if got.Counts.SA != 2 {
		t.Errorf("expected SA count 2, got %d", got.Counts.SA)
	}
	if got.Counts.ES != 1 {
		t.Errorf("expected ES count 1, got %d", got.Counts.ES)
	}
	if got.Counts.MT != 1 {
		t.Errorf("expected MT count 1, got %d", got.Counts.MT)
	}
	if got.Counts.NR != 1 {
		t.Errorf("expected NR count 1, got %d", got.Counts.NR)
	}
	if got.MCOptions.Min != 4 || got.MCOptions.Max != 4 {
		t.Errorf("expected MCOptions {4,4}, got %+v", got.MCOptions)
	}
	if got.MAOptions.Min != 4 || got.MAOptions.Max != 6 {
		t.Errorf("expected MAOptions {4,6}, got %+v", got.MAOptions)
	}
}

func TestEffectiveGeneration_AllFields(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Generation: config.Generation{
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
		Sources: []config.Source{
			{
				ID:  "s1",
				PDF: "s1.pdf",
				Generation: &config.Generation{
					Stages:      []config.Stage{config.StageTF, config.StageMC},
					Seed:        99,
					Provider:    "anthropic",
					APIKeyEnv:   "MY_KEY",
					Temperature: 0.9,
				},
			},
		},
	}
	got := c.EffectiveGeneration(&c.Sources[0])
	if len(got.Stages) != 2 || got.Stages[0] != config.StageTF {
		t.Errorf("expected stages [tf mc], got %v", got.Stages)
	}
	if got.Seed != 99 {
		t.Errorf("expected Seed 99, got %d", got.Seed)
	}
	if got.Provider != "anthropic" {
		t.Errorf("expected Provider anthropic, got %q", got.Provider)
	}
	if got.APIKeyEnv != "MY_KEY" {
		t.Errorf("expected APIKeyEnv MY_KEY, got %q", got.APIKeyEnv)
	}
	if got.Temperature != 0.9 {
		t.Errorf("expected Temperature 0.9, got %f", got.Temperature)
	}
}

func TestEffectiveValidation_AllFields(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Validation: config.Validation{
				MAMaxCorrectDensity: 0.5,
			},
		},
		Sources: []config.Source{
			{
				ID:  "s1",
				PDF: "s1.pdf",
				Validation: &config.Validation{
					EnforceUniqueOptionsPerQuestion: true,
					RequireSequentialNumbering:      true,
					RequireExactlyOneCorrectForTFMC: true,
				},
			},
		},
	}
	got := c.EffectiveValidation(&c.Sources[0])
	if !got.EnforceUniqueOptionsPerQuestion {
		t.Error("expected EnforceUniqueOptionsPerQuestion true")
	}
	if !got.RequireSequentialNumbering {
		t.Error("expected RequireSequentialNumbering true")
	}
	if !got.RequireExactlyOneCorrectForTFMC {
		t.Error("expected RequireExactlyOneCorrectForTFMC true")
	}
	// MAMaxCorrectDensity not overridden (0 in source)
	if got.MAMaxCorrectDensity != 0.5 {
		t.Errorf("expected MAMaxCorrectDensity 0.5, got %f", got.MAMaxCorrectDensity)
	}
}

func TestEffectiveQuiz_PartialMerge(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Quiz: config.Quiz{
				TitleTemplate: "default-title",
				Counts:        config.Counts{TF: 3, MA: 3, MC: 3},
			},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Quiz: &config.Quiz{TitleTemplate: "custom-title"}},
		},
	}
	got := c.EffectiveQuiz(&c.Sources[0])
	if got.TitleTemplate != "custom-title" {
		t.Errorf("expected custom-title, got %q", got.TitleTemplate)
	}
	// Counts not overridden, should keep defaults
	if got.Counts.TF != 3 {
		t.Errorf("expected TF count 3 from defaults, got %d", got.Counts.TF)
	}
}

func TestEffectiveGeneration_PartialMerge(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Generation: config.Generation{Model: "gpt-4o", Provider: "openai"},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Generation: &config.Generation{Model: "gpt-4o-mini"}},
		},
	}
	got := c.EffectiveGeneration(&c.Sources[0])
	if got.Model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %q", got.Model)
	}
	// Provider not overridden, should keep default
	if got.Provider != "openai" {
		t.Errorf("expected provider openai from defaults, got %q", got.Provider)
	}
}

func TestEffectiveValidation_PartialMerge(t *testing.T) {
	c := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Validation: config.Validation{MAMaxCorrectDensity: 0.5, RequireSequentialNumbering: true},
		},
		Sources: []config.Source{
			{ID: "s1", PDF: "s1.pdf", Validation: &config.Validation{MAMaxCorrectDensity: 0.3}},
		},
	}
	got := c.EffectiveValidation(&c.Sources[0])
	if got.MAMaxCorrectDensity != 0.3 {
		t.Errorf("expected MAMaxCorrectDensity 0.3, got %f", got.MAMaxCorrectDensity)
	}
	// RequireSequentialNumbering not overridden by source (false), should keep default true
	if !got.RequireSequentialNumbering {
		t.Error("expected RequireSequentialNumbering true from defaults")
	}
}
