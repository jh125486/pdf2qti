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
	// StageSA is the short answer (fill-in-the-blank) stage.
	StageSA Stage = "sa"
	// StageES is the essay stage.
	StageES Stage = "es"
	// StageMT is the matching stage.
	StageMT Stage = "mt"
	// StageNR is the numerical response stage.
	StageNR Stage = "nr"
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
	SA int `json:"sa,omitempty"`
	ES int `json:"es,omitempty"`
	MT int `json:"mt,omitempty"`
	NR int `json:"nr,omitempty"`
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
	OutDir       string `json:"outDir"`
	OpenReview   bool   `json:"openReview"`
	ReviewTarget string `json:"reviewTarget"`
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

// CourseObjective represents a single course-level learning objective.
type CourseObjective struct {
	CO   int    `json:"co"`
	Text string `json:"text"`
}

// Module groups one or more Sources (chapters) into a single unit for module-level
// documents (e.g. a combined Markdown doc and slide deck spanning all member chapters).
type Module struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SourceIDs []string `json:"sourceIds"`
}

// Config is the top-level configuration.
type Config struct {
	Version int `json:"version"`
	// CourseName is the course-wide display name (e.g. "CSCE 2120: Foundations of Computing"),
	// used to fill the subtitle placeholder on generated PPTX title slides.
	CourseName       string            `json:"courseName,omitempty"`
	CourseObjectives []CourseObjective `json:"courseObjectives,omitempty"`
	Defaults         Defaults          `json:"defaults"`
	Sources          []Source          `json:"sources"`
	Modules          []Module          `json:"modules,omitempty"`
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
	sourceIDs := make(map[string]bool, len(c.Sources))
	for i, s := range c.Sources {
		if s.ID == "" {
			return fmt.Errorf("sources[%d].id must not be empty", i)
		}
		if s.PDF == "" {
			return fmt.Errorf("sources[%d].pdf must not be empty", i)
		}
		sourceIDs[s.ID] = true
	}
	moduleIDs := make(map[string]bool, len(c.Modules))
	for i, m := range c.Modules {
		if m.ID == "" {
			return fmt.Errorf("modules[%d].id must not be empty", i)
		}
		if moduleIDs[m.ID] {
			return fmt.Errorf("modules[%d].id %q is duplicated", i, m.ID)
		}
		moduleIDs[m.ID] = true
		if m.Name == "" {
			return fmt.Errorf("modules[%d].name must not be empty", i)
		}
		if len(m.SourceIDs) == 0 {
			return fmt.Errorf("modules[%d].sourceIds must not be empty", i)
		}
		for _, id := range m.SourceIDs {
			if !sourceIDs[id] {
				return fmt.Errorf("modules[%d].sourceIds references unknown source %q", i, id)
			}
		}
	}
	return nil
}

// ModuleByID returns the Module with the given ID, or an error if none matches.
func (c *Config) ModuleByID(id string) (*Module, error) {
	for i := range c.Modules {
		if c.Modules[i].ID == id {
			return &c.Modules[i], nil
		}
	}
	return nil, fmt.Errorf("module %q not found", id)
}

// SourcesForModule returns the Sources referenced by m.SourceIDs, in order.
func (c *Config) SourcesForModule(m *Module) ([]*Source, error) {
	bySourceID := make(map[string]*Source, len(c.Sources))
	for i := range c.Sources {
		bySourceID[c.Sources[i].ID] = &c.Sources[i]
	}
	srcs := make([]*Source, 0, len(m.SourceIDs))
	for _, id := range m.SourceIDs {
		s, ok := bySourceID[id]
		if !ok {
			return nil, fmt.Errorf("module %q references unknown source %q", m.ID, id)
		}
		srcs = append(srcs, s)
	}
	return srcs, nil
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
	if sw.OpenReview {
		w.OpenReview = true
	}
	return w
}

// EffectiveQuiz returns the resolved Quiz for a source, merging non-zero source fields over defaults.
func (c *Config) EffectiveQuiz(s *Source) Quiz {
	q := c.Defaults.Quiz
	if s.Quiz == nil {
		return q
	}
	sq := s.Quiz
	if sq.TitleTemplate != "" {
		q.TitleTemplate = sq.TitleTemplate
	}
	if sq.DescriptionTemplate != "" {
		q.DescriptionTemplate = sq.DescriptionTemplate
	}
	if sq.Counts.TF != 0 {
		q.Counts.TF = sq.Counts.TF
	}
	if sq.Counts.MA != 0 {
		q.Counts.MA = sq.Counts.MA
	}
	if sq.Counts.MC != 0 {
		q.Counts.MC = sq.Counts.MC
	}
	if sq.Counts.SA != 0 {
		q.Counts.SA = sq.Counts.SA
	}
	if sq.Counts.ES != 0 {
		q.Counts.ES = sq.Counts.ES
	}
	if sq.Counts.MT != 0 {
		q.Counts.MT = sq.Counts.MT
	}
	if sq.Counts.NR != 0 {
		q.Counts.NR = sq.Counts.NR
	}
	if sq.MCOptions.Min != 0 {
		q.MCOptions.Min = sq.MCOptions.Min
	}
	if sq.MCOptions.Max != 0 {
		q.MCOptions.Max = sq.MCOptions.Max
	}
	if sq.MAOptions.Min != 0 {
		q.MAOptions.Min = sq.MAOptions.Min
	}
	if sq.MAOptions.Max != 0 {
		q.MAOptions.Max = sq.MAOptions.Max
	}
	return q
}

// EffectiveGeneration returns the resolved Generation for a source, merging non-zero source fields over defaults.
func (c *Config) EffectiveGeneration(s *Source) Generation {
	g := c.Defaults.Generation
	if s.Generation == nil {
		return g
	}
	sg := s.Generation
	if len(sg.Stages) > 0 {
		g.Stages = sg.Stages
	}
	if sg.Seed != 0 {
		g.Seed = sg.Seed
	}
	if sg.Provider != "" {
		g.Provider = sg.Provider
	}
	if sg.Model != "" {
		g.Model = sg.Model
	}
	if sg.APIKeyEnv != "" {
		g.APIKeyEnv = sg.APIKeyEnv
	}
	if sg.Temperature != 0 {
		g.Temperature = sg.Temperature
	}
	return g
}

// EffectiveValidation returns the resolved Validation for a source, merging non-zero source fields over defaults.
func (c *Config) EffectiveValidation(s *Source) Validation {
	v := c.Defaults.Validation
	if s.Validation == nil {
		return v
	}
	sv := s.Validation
	if sv.MAMaxCorrectDensity != 0 {
		v.MAMaxCorrectDensity = sv.MAMaxCorrectDensity
	}
	if sv.EnforceUniqueOptionsPerQuestion {
		v.EnforceUniqueOptionsPerQuestion = true
	}
	if sv.RequireSequentialNumbering {
		v.RequireSequentialNumbering = true
	}
	if sv.RequireExactlyOneCorrectForTFMC {
		v.RequireExactlyOneCorrectForTFMC = true
	}
	return v
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
