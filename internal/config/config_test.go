package config_test

import (
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
)

func TestLoad_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantErr    bool
		errLike    string
		wantVer    int
		wantNumSrc int
	}{
		{name: "valid", path: "testdata/valid.json", wantVer: 1, wantNumSrc: 1},
		{name: "missing file", path: "testdata/no_such_file.json", wantErr: true, errLike: "read config"},
		{name: "bad json", path: "testdata/bad_json.json", wantErr: true, errLike: "parse config"},
		{name: "invalid version", path: "testdata/invalid_version.json", wantErr: true, errLike: "validate config"},
		{name: "no sources", path: "testdata/no_sources.json", wantErr: true, errLike: "validate config"},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if !tt.wantErr {
				if cfg.Version != tt.wantVer {
					t.Errorf("version=%d want=%d", cfg.Version, tt.wantVer)
				}
				if len(cfg.Sources) != tt.wantNumSrc {
					t.Errorf("sources=%d want=%d", len(cfg.Sources), tt.wantNumSrc)
				}
			}
		})
	}
}

func TestValidate_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
		errLike string
	}{
		{
			name:    "valid",
			cfg:     config.Config{Version: 1, Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}}},
			wantErr: false,
		},
		{
			name:    "version not 1",
			cfg:     config.Config{Version: 2, Sources: []config.Source{{ID: "x", PDF: "x.pdf"}}},
			wantErr: true,
			errLike: "unsupported config version",
		},
		{
			name:    "empty sources",
			cfg:     config.Config{Version: 1, Sources: []config.Source{}},
			wantErr: true,
			errLike: "sources must not be empty",
		},
		{
			name:    "missing source id",
			cfg:     config.Config{Version: 1, Sources: []config.Source{{PDF: "x.pdf"}}},
			wantErr: true,
			errLike: "id must not be empty",
		},
		{
			name:    "missing source pdf",
			cfg:     config.Config{Version: 1, Sources: []config.Source{{ID: "x"}}},
			wantErr: true,
			errLike: "pdf must not be empty",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}

func TestEffectiveWorkflow_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		cfg              config.Config
		wantOutDir       string
		wantOpenReview   bool
		wantReviewTarget string
	}{
		{
			name: "no source override",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{OutDir: "default-out"}},
				Sources:  []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			wantOutDir: "default-out",
		},
		{
			name: "full merge",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{OutDir: "default-out", ReviewTarget: "default-target"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{OutDir: "source-out", OpenReview: true}},
				},
			},
			wantOutDir:       "source-out",
			wantOpenReview:   true,
			wantReviewTarget: "default-target",
		},
		{
			name: "review target override",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{ReviewTarget: "default-target"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{ReviewTarget: "source-target"}},
				},
			},
			wantReviewTarget: "source-target",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.EffectiveWorkflow(&tt.cfg.Sources[0])
			if got.OutDir != tt.wantOutDir {
				t.Errorf("OutDir=%q want=%q", got.OutDir, tt.wantOutDir)
			}
			if got.OpenReview != tt.wantOpenReview {
				t.Errorf("OpenReview=%v want=%v", got.OpenReview, tt.wantOpenReview)
			}
			if got.ReviewTarget != tt.wantReviewTarget {
				t.Errorf("ReviewTarget=%q want=%q", got.ReviewTarget, tt.wantReviewTarget)
			}
		})
	}
}

func TestEffectiveQuiz_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cfg               config.Config
		wantTitleTemplate string
		wantDescTemplate  string
		wantCounts        config.Counts
		wantMCOptions     config.OptionRange
		wantMAOptions     config.OptionRange
	}{
		{
			name: "no source override",
			cfg: config.Config{
				Defaults: config.Defaults{Quiz: config.Quiz{TitleTemplate: "default"}},
				Sources:  []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			wantTitleTemplate: "default",
		},
		{
			name: "source override",
			cfg: config.Config{
				Defaults: config.Defaults{Quiz: config.Quiz{TitleTemplate: "default"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Quiz: &config.Quiz{TitleTemplate: "custom", Counts: config.Counts{TF: 5, MA: 5, MC: 5}}},
				},
			},
			wantTitleTemplate: "custom",
			wantCounts:        config.Counts{TF: 5, MA: 5, MC: 5},
		},
		{
			name: "partial merge keeps default counts",
			cfg: config.Config{
				Defaults: config.Defaults{Quiz: config.Quiz{TitleTemplate: "default-title", Counts: config.Counts{TF: 3, MA: 3, MC: 3}}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Quiz: &config.Quiz{TitleTemplate: "custom-title"}},
				},
			},
			wantTitleTemplate: "custom-title",
			wantCounts:        config.Counts{TF: 3, MA: 3, MC: 3},
		},
		{
			name: "all fields overridden",
			cfg: config.Config{
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
			},
			wantTitleTemplate: "default-title",
			wantDescTemplate:  "src-desc",
			wantCounts:        config.Counts{TF: 1, MA: 1, MC: 1, SA: 2, ES: 1, MT: 1, NR: 1},
			wantMCOptions:     config.OptionRange{Min: 4, Max: 4},
			wantMAOptions:     config.OptionRange{Min: 4, Max: 6},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.EffectiveQuiz(&tt.cfg.Sources[0])
			if got.TitleTemplate != tt.wantTitleTemplate {
				t.Errorf("TitleTemplate=%q want=%q", got.TitleTemplate, tt.wantTitleTemplate)
			}
			if got.DescriptionTemplate != tt.wantDescTemplate {
				t.Errorf("DescriptionTemplate=%q want=%q", got.DescriptionTemplate, tt.wantDescTemplate)
			}
			if got.Counts != tt.wantCounts {
				t.Errorf("Counts=%+v want=%+v", got.Counts, tt.wantCounts)
			}
			if tt.wantMCOptions != (config.OptionRange{}) && got.MCOptions != tt.wantMCOptions {
				t.Errorf("MCOptions=%+v want=%+v", got.MCOptions, tt.wantMCOptions)
			}
			if tt.wantMAOptions != (config.OptionRange{}) && got.MAOptions != tt.wantMAOptions {
				t.Errorf("MAOptions=%+v want=%+v", got.MAOptions, tt.wantMAOptions)
			}
		})
	}
}

func TestEffectiveGeneration_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cfg          config.Config
		wantModel    string
		wantProvider string
		wantSeed     int
		wantStages   []config.Stage
	}{
		{
			name: "no source override",
			cfg: config.Config{
				Defaults: config.Defaults{Generation: config.Generation{Model: "gpt-4o"}},
				Sources:  []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			wantModel: "gpt-4o",
		},
		{
			name: "source override",
			cfg: config.Config{
				Defaults: config.Defaults{Generation: config.Generation{Model: "gpt-4o"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Generation: &config.Generation{Model: "gpt-4o-mini"}},
				},
			},
			wantModel: "gpt-4o-mini",
		},
		{
			name: "partial merge keeps default provider",
			cfg: config.Config{
				Defaults: config.Defaults{Generation: config.Generation{Model: "gpt-4o", Provider: "openai"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Generation: &config.Generation{Model: "gpt-4o-mini"}},
				},
			},
			wantModel:    "gpt-4o-mini",
			wantProvider: "openai",
		},
		{
			name: "all fields overridden",
			cfg: config.Config{
				Defaults: config.Defaults{Generation: config.Generation{Model: "gpt-4o"}},
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
			},
			wantProvider: "anthropic",
			wantSeed:     99,
			wantStages:   []config.Stage{config.StageTF, config.StageMC},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.EffectiveGeneration(&tt.cfg.Sources[0])
			if tt.wantModel != "" && got.Model != tt.wantModel {
				t.Errorf("Model=%q want=%q", got.Model, tt.wantModel)
			}
			if tt.wantProvider != "" && got.Provider != tt.wantProvider {
				t.Errorf("Provider=%q want=%q", got.Provider, tt.wantProvider)
			}
			if tt.wantSeed != 0 && got.Seed != tt.wantSeed {
				t.Errorf("Seed=%d want=%d", got.Seed, tt.wantSeed)
			}
			if len(tt.wantStages) > 0 && len(got.Stages) != len(tt.wantStages) {
				t.Errorf("Stages=%v want=%v", got.Stages, tt.wantStages)
			}
		})
	}
}

func TestEffectiveValidation_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		cfg                 config.Config
		wantMAMaxDensity    float64
		wantEnforceUnique   bool
		wantRequireSeq      bool
		wantRequireExactOne bool
	}{
		{
			name: "no source override",
			cfg: config.Config{
				Defaults: config.Defaults{Validation: config.Validation{MAMaxCorrectDensity: 0.5}},
				Sources:  []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			wantMAMaxDensity: 0.5,
		},
		{
			name: "source override",
			cfg: config.Config{
				Defaults: config.Defaults{Validation: config.Validation{MAMaxCorrectDensity: 0.5}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Validation: &config.Validation{MAMaxCorrectDensity: 0.3}},
				},
			},
			wantMAMaxDensity: 0.3,
		},
		{
			name: "partial merge keeps default flag",
			cfg: config.Config{
				Defaults: config.Defaults{Validation: config.Validation{MAMaxCorrectDensity: 0.5, RequireSequentialNumbering: true}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Validation: &config.Validation{MAMaxCorrectDensity: 0.3}},
				},
			},
			wantMAMaxDensity: 0.3,
			wantRequireSeq:   true,
		},
		{
			name: "all fields overridden",
			cfg: config.Config{
				Defaults: config.Defaults{Validation: config.Validation{MAMaxCorrectDensity: 0.5}},
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
			},
			wantMAMaxDensity:    0.5,
			wantEnforceUnique:   true,
			wantRequireSeq:      true,
			wantRequireExactOne: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.EffectiveValidation(&tt.cfg.Sources[0])
			if got.MAMaxCorrectDensity != tt.wantMAMaxDensity {
				t.Errorf("MAMaxCorrectDensity=%f want=%f", got.MAMaxCorrectDensity, tt.wantMAMaxDensity)
			}
			if got.EnforceUniqueOptionsPerQuestion != tt.wantEnforceUnique {
				t.Errorf("EnforceUniqueOptionsPerQuestion=%v want=%v", got.EnforceUniqueOptionsPerQuestion, tt.wantEnforceUnique)
			}
			if got.RequireSequentialNumbering != tt.wantRequireSeq {
				t.Errorf("RequireSequentialNumbering=%v want=%v", got.RequireSequentialNumbering, tt.wantRequireSeq)
			}
			if got.RequireExactlyOneCorrectForTFMC != tt.wantRequireExactOne {
				t.Errorf("RequireExactlyOneCorrectForTFMC=%v want=%v", got.RequireExactlyOneCorrectForTFMC, tt.wantRequireExactOne)
			}
		})
	}
}

func TestOutDir_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      config.Config
		expected string
	}{
		{
			name: "source outDir wins",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{OutDir: "default-out"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{OutDir: "src-out"}},
				},
			},
			expected: "src-out",
		},
		{
			name: "defaults outDir",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{OutDir: "default-out"}},
				Sources:  []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			expected: "default-out",
		},
		{
			name: "fallback to out",
			cfg: config.Config{
				Sources: []config.Source{{ID: "s1", PDF: "s1.pdf"}},
			},
			expected: "out",
		},
		{
			name: "source workflow present but outDir empty falls back to defaults",
			cfg: config.Config{
				Defaults: config.Defaults{Workflow: config.Workflow{OutDir: "default-out"}},
				Sources: []config.Source{
					{ID: "s1", PDF: "s1.pdf", Workflow: &config.Workflow{ReviewTarget: "editor"}},
				},
			},
			expected: "default-out",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.OutDir(&tt.cfg.Sources[0])
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
