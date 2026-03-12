package distill_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

// stubLLM implements LLM for testing.
type stubLLM struct {
	response string
	err      error
}

func (s *stubLLM) Complete(_ context.Context, _ string) (string, error) {
	return s.response, s.err
}

func validLLMResponse() string {
	dc := distill.DistilledContext{
		ModuleName:       "Signals",
		Text:             "thick distilled text",
		Overview:         "<p>We examine signals.</p>",
		KeyConcepts:      []string{"SIGINT", "sigaction()"},
		MaterialOverview: "Chapter covers signals.",
		TeachingNotes:    "Teach why, not what.",
		Objectives: []distill.Objective{
			{CO: 1, Text: "Write robust software."},
		},
	}
	b, _ := json.Marshal(dc)
	return string(b)
}

func TestDistill_HappyPath(t *testing.T) {
	src := &config.Source{ID: "ch21", Name: "TLPI", Chapter: 21, PDF: "ch21.pdf"}
	objectives := []config.CourseObjective{
		{CO: 1, Text: "Write robust software."},
	}
	llm := &stubLLM{response: validLLMResponse()}

	dc, err := distill.Distill(context.Background(), src, objectives, llm, "chapter text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dc.SourceID != "ch21" {
		t.Errorf("expected source_id %q, got %q", "ch21", dc.SourceID)
	}
	if dc.Book != "TLPI" {
		t.Errorf("expected book %q, got %q", "TLPI", dc.Book)
	}
	if dc.Chapter != 21 {
		t.Errorf("expected chapter 21, got %d", dc.Chapter)
	}
	if dc.ModuleName != "Signals" {
		t.Errorf("expected module_name %q, got %q", "Signals", dc.ModuleName)
	}
	if len(dc.KeyConcepts) != 2 {
		t.Errorf("expected 2 key_concepts, got %d", len(dc.KeyConcepts))
	}
}

func TestDistill_LLMError(t *testing.T) {
	src := &config.Source{ID: "ch21", Name: "TLPI", Chapter: 21, PDF: "ch21.pdf"}
	llm := &stubLLM{err: errors.New("llm failed")}

	_, err := distill.Distill(context.Background(), src, nil, llm, "text")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "llm complete") {
		t.Errorf("expected 'llm complete' in error, got: %v", err)
	}
}

func TestDistill_InvalidJSON(t *testing.T) {
	src := &config.Source{ID: "ch21", Name: "TLPI", Chapter: 21, PDF: "ch21.pdf"}
	llm := &stubLLM{response: "not json"}

	_, err := distill.Distill(context.Background(), src, nil, llm, "text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "parse llm response") {
		t.Errorf("expected 'parse llm response' in error, got: %v", err)
	}
}

func TestLoad_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.json")

	dc := &distill.DistilledContext{
		SourceID:         "ch21",
		Book:             "TLPI",
		Chapter:          21,
		ModuleName:       "Signals",
		Text:             "some text",
		Overview:         "<p>We cover signals.</p>",
		KeyConcepts:      []string{"SIGINT"},
		MaterialOverview: "Intro to signals.",
		TeachingNotes:    "Focus on why.",
		Objectives: []distill.Objective{
			{CO: 1, Text: "Write software."},
		},
	}
	if err := distill.Save(path, dc); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := distill.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.SourceID != dc.SourceID {
		t.Errorf("expected source_id %q, got %q", dc.SourceID, loaded.SourceID)
	}
	if loaded.Chapter != dc.Chapter {
		t.Errorf("expected chapter %d, got %d", dc.Chapter, loaded.Chapter)
	}
	if len(loaded.Objectives) != 1 {
		t.Errorf("expected 1 objective, got %d", len(loaded.Objectives))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := distill.Load("/no/such/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read context") {
		t.Errorf("expected 'read context' in error, got: %v", err)
	}
}

func TestLoad_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.json")
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := distill.Load(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !strings.Contains(err.Error(), "parse context") {
		t.Errorf("expected 'parse context' in error, got: %v", err)
	}
}

func TestSave_WritesJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.json")
	dc := &distill.DistilledContext{SourceID: "ch21", Book: "TLPI", Chapter: 21}

	if err := distill.Save(path, dc); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"source_id"`) {
		t.Error("expected 'source_id' in saved JSON")
	}
	if !strings.Contains(string(data), `"TLPI"`) {
		t.Error("expected 'TLPI' in saved JSON")
	}
}

func TestSave_PermissionError(t *testing.T) {
	err := distill.Save("/no/such/dir/ctx.json", &distill.DistilledContext{})
	if err == nil {
		t.Fatal("expected error for bad path")
	}
	if !strings.Contains(err.Error(), "write context") {
		t.Errorf("expected 'write context' in error, got: %v", err)
	}
}
