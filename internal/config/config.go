// Package config provides configuration loading and validation for pdf2qti.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Stage represents a quiz generation stage.
type Stage string

const (
	// StageTF is the true/false stage.
	StageTF Stage = "tf"
	// StageMA is the multiple-answer stage.
	StageMA Stage = "ma"
	// StageMC is the multiple-choice stage.
	StageMC Stage = "mc"
)

// OptionRange defines min/max option counts.
type OptionRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Counts defines question counts per stage.
type Counts struct {
	TF int `json:"tf"`
	MA int `json:"ma"`
	MC int `json:"mc"`
}

// Quiz holds quiz generation parameters.
type Quiz struct {
	TitleTemplate       string      `json:"titleTemplate"`
	DescriptionTemplate string      `json:"descriptionTemplate,omitempty"`
	Counts              Counts      `json:"counts"`
	MCOptions           OptionRange `json:"mcOptions"`
	MAOptions           OptionRange `json:"maOptions"`
}

// Generation holds LLM generation parameters.
type Generation struct {
	Stages      []Stage `json:"stages"`
	Seed        int     `json:"seed"`
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	APIKeyEnv   string  `json:"apiKeyEnv"`
	Temperature float64 `json:"temperature,omitempty"`
}

// Validation holds quiz validation rules.
type Validation struct {
	MAMaxCorrectDensity             float64 `json:"maMaxCorrectDensity"`
	EnforceUniqueOptionsPerQuestion bool    `json:"enforceUniqueOptionsPerQuestion"`
	RequireSequentialNumbering      bool    `json:"requireSequentialNumbering"`
	RequireExactlyOneCorrectForTFMC bool    `json:"requireExactlyOneCorrectForTfMc"`
}

// Workflow holds workflow parameters.
type Workflow struct {
	OutDir                     string `json:"outDir"`
	OpenReview                 bool   `json:"openReview"`
	ReviewTarget               string `json:"reviewTarget"`
	RequireApprovalBeforeBuild bool   `json:"requireApprovalBeforeBuild,omitempty"`
	PackageZip                 bool   `json:"packageZip,omitempty"`
}

// Defaults holds global defaults.
type Defaults struct {
	Quiz       Quiz       `json:"quiz"`
	Generation Generation `json:"generation"`
	Validation Validation `json:"validation"`
	Workflow   Workflow   `json:"workflow"`
}

// Source represents a single PDF source.
type Source struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Chapter    int         `json:"chapter"`
	PDF        string      `json:"pdf"`
	Quiz       *Quiz       `json:"quiz,omitempty"`
	Generation *Generation `json:"generation,omitempty"`
	Validation *Validation `json:"validation,omitempty"`
	Workflow   *Workflow   `json:"workflow,omitempty"`
}

// Config is the top-level configuration.
type Config struct {
	Version  int      `json:"version"`
	Defaults Defaults `json:"defaults"`
	Sources  []Source `json:"sources"`
}

// Load reads and parses a JSON config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks the config for required fields and constraints.
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d (expected 1)", c.Version)
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("sources must not be empty")
	}
	for i, s := range c.Sources {
		if s.ID == "" {
			return fmt.Errorf("sources[%d].id must not be empty", i)
		}
		if s.PDF == "" {
			return fmt.Errorf("sources[%d].pdf must not be empty", i)
		}
	}
	return nil
}

// EffectiveWorkflow returns the resolved Workflow for a source, merging source overrides over defaults.
func (c *Config) EffectiveWorkflow(s *Source) Workflow {
	w := c.Defaults.Workflow
	if s.Workflow == nil {
		return w
	}
	sw := s.Workflow
	if sw.OutDir != "" {
		w.OutDir = sw.OutDir
	}
	if sw.ReviewTarget != "" {
		w.ReviewTarget = sw.ReviewTarget
	}
	if sw.OpenReview != w.OpenReview {
		w.OpenReview = sw.OpenReview
	}
	if sw.PackageZip != w.PackageZip {
		w.PackageZip = sw.PackageZip
	}
	if sw.RequireApprovalBeforeBuild != w.RequireApprovalBeforeBuild {
		w.RequireApprovalBeforeBuild = sw.RequireApprovalBeforeBuild
	}
	return w
}

// EffectiveQuiz returns the resolved Quiz for a source.
func (c *Config) EffectiveQuiz(s *Source) Quiz {
	if s.Quiz != nil {
		return *s.Quiz
	}
	return c.Defaults.Quiz
}

// EffectiveGeneration returns the resolved Generation for a source.
func (c *Config) EffectiveGeneration(s *Source) Generation {
	if s.Generation != nil {
		return *s.Generation
	}
	return c.Defaults.Generation
}

// EffectiveValidation returns the resolved Validation for a source.
func (c *Config) EffectiveValidation(s *Source) Validation {
	if s.Validation != nil {
		return *s.Validation
	}
	return c.Defaults.Validation
}

// OutDir returns the effective output directory for a source, falling back to "out".
func (c *Config) OutDir(s *Source) string {
	if s.Workflow != nil && s.Workflow.OutDir != "" {
		return s.Workflow.OutDir
	}
	if c.Defaults.Workflow.OutDir != "" {
		return c.Defaults.Workflow.OutDir
	}
	return "out"
}
