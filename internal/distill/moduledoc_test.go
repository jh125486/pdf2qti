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

// splitLLM answers BuildModuleDoc's JSON-merge prompt directly, and delegates its three
// proto-deck prompt shapes (outline, batch expansion, summary) to protoDeckStubLLM.
type splitLLM struct {
	mergeResp    string
	mergeErr     error
	deckErr      error // if set, every proto-deck-shaped call fails with this
	deck         protoDeckStubLLM
	sawDeckCall  bool
	sawMergeCall bool
}

func (s *splitLLM) Complete(ctx context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "planning a prototype PowerPoint outline") ||
		strings.Contains(prompt, "writing the bullet content for") ||
		strings.Contains(prompt, "exactly one summary bullet per agenda item") {
		s.sawDeckCall = true
		if s.deckErr != nil {
			return "", s.deckErr
		}
		return s.deck.Complete(ctx, prompt)
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

type buildModuleDocTestCase struct {
	name     string
	llm      *splitLLM
	chapters func() []*distill.DistilledContext // nil -> sampleChapters()
	wantErr  bool
	errLike  string
	check    func(t *testing.T, doc *distill.ModuleDoc)
}

// buildModuleDocTestCases builds TestBuildModuleDoc_Table's table. Split out from the test
// function itself to keep gocyclo's complexity count on the (trivial) runner, not this literal.
func buildModuleDocTestCases() []buildModuleDocTestCase {
	return []buildModuleDocTestCase{
		{
			name: "happy path",
			llm:  &splitLLM{mergeResp: validMergeResp},
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
			llm:     &splitLLM{mergeErr: errors.New("boom")},
			wantErr: true,
			errLike: "llm complete",
		},
		{
			name:    "merge invalid json",
			llm:     &splitLLM{mergeResp: "not json"},
			wantErr: true,
			errLike: "parse llm response",
		},
		{
			name:    "deck generation error",
			llm:     &splitLLM{mergeResp: validMergeResp, deckErr: errors.New("deck boom")},
			wantErr: true,
			errLike: "generate proto deck",
		},
		{
			// Chapters with an empty ModuleName fall back to Book for the section's ChapterName.
			name: "chapter with empty module name falls back to book",
			llm:  &splitLLM{mergeResp: validMergeResp},
			chapters: func() []*distill.DistilledContext {
				chapters := sampleChapters()
				chapters[0].ModuleName = ""
				return chapters
			},
			check: func(t *testing.T, doc *distill.ModuleDoc) {
				t.Helper()
				if doc.Sections[0].ChapterName != "Chapter 1" {
					t.Fatalf("expected ChapterName to fall back to Book %q, got %q", "Chapter 1", doc.Sections[0].ChapterName)
				}
			},
		},
	}
}

func TestBuildModuleDoc_Table(t *testing.T) {
	t.Parallel()

	for _, tt := range buildModuleDocTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chapters := sampleChapters()
			if tt.chapters != nil {
				chapters = tt.chapters()
			}
			doc, err := distill.BuildModuleDoc(context.Background(), tt.llm, "mod1", "Module 1", chapters, 3, 8)
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
		{
			name: "save markdown success",
			run: func(t *testing.T, dir string) error {
				t.Helper()
				path := filepath.Join(dir, "mod.md")
				if err := distill.SaveModuleMarkdown(path, "# Module 1"); err != nil {
					return err
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "# Module 1" {
					t.Fatalf("got %q, want %q", got, "# Module 1")
				}
				return nil
			},
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
