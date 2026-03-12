// Package distill provides LLM-based distillation of PDF chapter text into
// structured JSON context files consumed by the generate and page commands.
package distill

import (
	"encoding/json"
	"fmt"
	"os"
)

// Objective is a course objective that applies to this chapter.
type Objective struct {
	CO   int    `json:"co"`
	Text string `json:"text"`
}

// DistilledContext holds the structured output produced by the distill command.
type DistilledContext struct {
	SourceID         string      `json:"source_id"`
	Book             string      `json:"book"`
	Chapter          int         `json:"chapter"`
	ModuleName       string      `json:"module_name"`
	Text             string      `json:"text"`
	Overview         string      `json:"overview"`
	KeyConcepts      []string    `json:"key_concepts"`
	MaterialOverview string      `json:"material_overview"`
	TeachingNotes    string      `json:"teaching_notes"`
	Objectives       []Objective `json:"objectives"`
}

// Load reads and parses a DistilledContext from a JSON file at path.
func Load(path string) (*DistilledContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read context %q: %w", path, err)
	}
	var dc DistilledContext
	if err := json.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("parse context %q: %w", path, err)
	}
	return &dc, nil
}

// Save writes dc as JSON to path, creating or truncating the file.
func Save(path string, dc *DistilledContext) error {
	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write context %q: %w", path, err)
	}
	return nil
}
