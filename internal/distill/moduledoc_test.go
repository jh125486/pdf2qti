package distill_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
)

// splitLLM answers two different prompt shapes from one Complete method, mirroring how
// BuildModuleDoc issues both a JSON-merge prompt and the proto-deck markdown prompt.
type splitLLM struct {
	deckMarker   string
	mergeResp    string
	mergeErr     error
	deckResp     string
	deckErr      error
	sawDeckCall  bool
	sawMergeCall bool
}

func (s *splitLLM) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, s.deckMarker) {
		s.sawDeckCall = true
		return s.deckResp, s.deckErr
	}
	s.sawMergeCall = true
	return s.mergeResp, s.mergeErr
}

func sampleChapters() []*distill.DistilledContext {
	return []*distill.DistilledContext{
		{
			SourceID: "ch1", Book: "Chapter 1", Chapter: 1, ModuleName: "Processes",
			Overview: "o1", KeyConcepts: []string{"fork"},
			Sections: []distill.Section{{Title: "Intro", Summary: "s1"}},
		},
		{
			SourceID: "ch2", Book: "Chapter 2", Chapter: 2, ModuleName: "Signals",
			Overview: "o2", KeyConcepts: []string{"kill"},
			Sections: []distill.Section{{Title: "Delivery", Summary: "s2"}, {Title: "Handlers", Summary: "s3"}},
		},
	}
}

const validMergeResp = `{"overview":"combined overview","objectives":[{"co":1,"text":"obj"}],"vocabulary":[{"term":"fork","definition":"def"}],"theorems":[]}`

func TestBuildModuleDoc_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		llm     *splitLLM
		wantErr bool
		errLike string
		check   func(t *testing.T, doc *distill.ModuleDoc)
	}{
		{
			name: "happy path",
			llm:  &splitLLM{deckMarker: "prototype PowerPoint outline", mergeResp: validMergeResp, deckResp: validDeck(4)},
			check: func(t *testing.T, doc *distill.ModuleDoc) {
				t.Helper()
				if doc.Overview != "combined overview" {
					t.Errorf("Overview=%q", doc.Overview)
				}
				if len(doc.Objectives) != 1 || len(doc.Vocabulary) != 1 {
					t.Errorf("unexpected merge result: %+v", doc)
				}
				if len(doc.Sections) != 3 {
					t.Fatalf("expected 3 flattened sections, got %d", len(doc.Sections))
				}
				if doc.Sections[0].ChapterTag != "ch1" || doc.Sections[2].ChapterTag != "ch2" {
					t.Errorf("unexpected chapter tagging: %+v", doc.Sections)
				}
				if doc.SlidesMD == "" {
					t.Error("expected non-empty SlidesMD")
				}
			},
		},
		{
			name:    "merge llm error",
			llm:     &splitLLM{deckMarker: "prototype PowerPoint outline", mergeErr: errors.New("boom")},
			wantErr: true,
			errLike: "llm complete",
		},
		{
			name:    "merge invalid json",
			llm:     &splitLLM{deckMarker: "prototype PowerPoint outline", mergeResp: "not json"},
			wantErr: true,
			errLike: "parse llm response",
		},
		{
			name:    "deck generation error",
			llm:     &splitLLM{deckMarker: "prototype PowerPoint outline", mergeResp: validMergeResp, deckErr: errors.New("deck boom")},
			wantErr: true,
			errLike: "generate proto deck",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := distill.BuildModuleDoc(context.Background(), tt.llm, "mod1", "Module 1", sampleChapters(), 3, 8)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
			if tt.check != nil {
				tt.check(t, doc)
			}
		})
	}
}

func TestBuildModuleDoc_NoChapters(t *testing.T) {
	t.Parallel()

	_, err := distill.BuildModuleDoc(context.Background(), &splitLLM{}, "mod1", "Module 1", nil, 3, 8)
	if err == nil || !strings.Contains(err.Error(), "no chapters") {
		t.Fatalf("expected 'no chapters' error, got %v", err)
	}
}

func TestRenderModuleMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("with theorems", func(t *testing.T) {
		t.Parallel()
		doc := &distill.ModuleDoc{
			ModuleName: "Module 1",
			Overview:   "overview text",
			Objectives: []distill.Objective{{CO: 1, Text: "do a thing"}},
			Vocabulary: []distill.VocabTerm{{Term: "fork", Definition: "spawn a process"}},
			Theorems:   []distill.Theorem{{Name: "Amdahl's Law", Statement: "speedup is bounded"}},
			Sections: []distill.TaggedSection{
				{ChapterTag: "ch1", ChapterName: "Chapter 1", Title: "Intro", Summary: "s1"},
			},
			SlidesMD: "<!-- meta: 1 agenda -->\n# Agenda\n",
		}
		md := distill.RenderModuleMarkdown(doc)
		for _, want := range []string{
			"# Module 1", "## Overview", "overview text",
			"## Learning Objectives", "CO1: do a thing",
			"## Vocabulary", "**fork**", "## Useful Theorems", "Amdahl's Law",
			"## Sections", "### Chapter 1", "#### Intro", "## Slides", "meta: 1 agenda",
		} {
			if !strings.Contains(md, want) {
				t.Errorf("expected markdown to contain %q, got:\n%s", want, md)
			}
		}
	})

	t.Run("without theorems", func(t *testing.T) {
		t.Parallel()
		doc := &distill.ModuleDoc{ModuleName: "Module 1", Overview: "o"}
		md := distill.RenderModuleMarkdown(doc)
		if strings.Contains(md, "Useful Theorems") {
			t.Errorf("expected Theorems heading to be omitted when empty, got:\n%s", md)
		}
	})
}

func TestSaveLoadModuleDoc_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(t *testing.T, dir string) error
		wantErr bool
		errLike string
	}{
		{
			name: "save and load",
			run: func(t *testing.T, dir string) error {
				t.Helper()
				path := filepath.Join(dir, "mod.json")
				doc := &distill.ModuleDoc{ModuleID: "m1", ModuleName: "Module 1"}
				if err := distill.SaveModuleDoc(path, doc); err != nil {
					return err
				}
				loaded, err := distill.LoadModuleDoc(path)
				if err != nil {
					return err
				}
				if loaded.ModuleID != doc.ModuleID {
					t.Fatalf("module id mismatch: %q vs %q", loaded.ModuleID, doc.ModuleID)
				}
				return nil
			},
		},
		{
			name: "load missing file",
			run: func(_ *testing.T, _ string) error {
				_, err := distill.LoadModuleDoc("/no/such/file.json")
				return err
			},
			wantErr: true,
			errLike: "read module doc",
		},
		{
			name: "load corrupt json",
			run: func(t *testing.T, dir string) error {
				t.Helper()
				path := filepath.Join(dir, "mod.json")
				if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err := distill.LoadModuleDoc(path)
				return err
			},
			wantErr: true,
			errLike: "parse module doc",
		},
		{
			name: "save bad path",
			run: func(_ *testing.T, _ string) error {
				return distill.SaveModuleDoc("/no/such/dir/mod.json", &distill.ModuleDoc{})
			},
			wantErr: true,
			errLike: "write module doc",
		},
		{
			name: "save markdown bad path",
			run: func(_ *testing.T, _ string) error {
				return distill.SaveModuleMarkdown("/no/such/dir/mod.md", "content")
			},
			wantErr: true,
			errLike: "write module markdown",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.run(t, t.TempDir())
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}
