package commands_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
	"github.com/jh125486/pdf2qti/internal/distill"
)

// execTestMu serializes tests that mutate the process-global os.Args to call
// commands.Execute. None of these tests call t.Parallel(), so Go's test
// scheduler already runs them to completion before any parallel subtests
// resume; this mutex is a defensive belt-and-suspenders guard so that a
// future accidental t.Parallel() on one of these tests can't introduce a
// data race on os.Args.
var execTestMu sync.Mutex

// withArgs sets os.Args for the duration of the calling test, serialized
// against other os.Args-mutating tests, and restores the original value
// (and releases the lock) via t.Cleanup.
func withArgs(t *testing.T, args []string) {
	t.Helper()
	execTestMu.Lock()
	origArgs := os.Args
	os.Args = args
	t.Cleanup(func() {
		os.Args = origArgs
		execTestMu.Unlock()
	})
}

// mustWriteFile writes data to path, failing the test immediately on error. A thin wrapper
// around os.WriteFile so table-building helpers don't each carry their own
// if err != nil { t.Fatal(err) } branch — those add up fast across a table with many fixture
// writes and push the enclosing function over gocyclo's complexity threshold for no real benefit.
func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

const validQuizMD = `# Test Quiz

## MC

1. What is 2+2?
[ ] 3
[*] 4
[ ] 5
`

const validationFailQuizMD = `# Test Quiz

## MC

2. What is 2+2?
[*] 4
`

func writeContextFile(t *testing.T, dir string) {
	t.Helper()
	const minCtx = `{"source_id":"","module_name":"","text":"","overview":"","key_concepts":[],"material_overview":"","teaching_notes":"","objectives":[]}`
	path := filepath.Join(dir, "src01_context.json")
	if err := os.WriteFile(path, []byte(minCtx), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeConfigFile(t *testing.T, dir, pdfPath string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "quiz.json")
	cfgJSON := fmt.Sprintf(`{"version":1,"defaults":{"workflow":{"outDir":%q}},"sources":[{"id":"src01","name":"Chapter 1","chapter":1,"pdf":%q}]}`,
		dir, pdfPath)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func writeDistilledContextFile(t *testing.T, outDir string) {
	t.Helper()
	writeDistilledContextFileWithID(t, outDir, "src01")
}

func writeDistilledContextFileWithID(t *testing.T, outDir, sourceID string) {
	t.Helper()
	writeDistilledContextFileWithSections(t, outDir, sourceID, []distill.Section{{Title: "Intro", Summary: "summary"}})
}

// writeDistilledContextFileWithSections is writeDistilledContextFileWithID but with a Sections
// field of the caller's choosing (nil/empty, one entry, or several) — outline planning itself
// doesn't consume Sections (it chunks Text by character count, see generateOutlineChunked's doc
// comment), so this exercises the DistilledContext.Sections through to
// ProtoChapterInput.Sections wiring at the command layer, not any outline-planning behavior.
// Text is a small non-empty placeholder ("t") so generateOutlineChunked's chunking always has at
// least one chunk to plan, regardless of how many Sections the caller passes.
func writeDistilledContextFileWithSections(t *testing.T, outDir, sourceID string, sections []distill.Section) {
	t.Helper()
	dc := &distill.DistilledContext{
		SourceID:         sourceID,
		Book:             "Book",
		Chapter:          1,
		ModuleName:       "Module 1",
		Overview:         "<p>Overview</p>",
		MaterialOverview: "Read this",
		KeyConcepts:      []string{"pipes"},
		Text:             "t",
		Sections:         sections,
		Agenda:           []string{"Topic A", "Topic B", "Topic C"},
		Slides: []distill.Slide{
			{Title: "Topic A", Content: "Point 1\nPoint 2"},
			{Title: "Topic B", Content: "Point 1"},
			{Title: "Topic C", Content: "Point 1\nPoint 2"},
		},
	}
	if err := distill.Save(filepath.Join(outDir, sourceID+"_context.json"), dc); err != nil {
		t.Fatal(err)
	}
}

// writeDistilledContextFileWithText is writeDistilledContextFileWithID but with a Text field of
// the caller's choosing, so tests can drive distill.AutoSlideRange's chars-of-Text-based sizing
// to a known, non-default target instead of the empty-Text minimum it gets otherwise.
func writeDistilledContextFileWithText(t *testing.T, outDir, sourceID, text string) {
	t.Helper()
	dc := &distill.DistilledContext{
		SourceID:         sourceID,
		Book:             "Book",
		Chapter:          1,
		ModuleName:       "Module 1",
		Overview:         "<p>Overview</p>",
		MaterialOverview: "Read this",
		KeyConcepts:      []string{"pipes"},
		Text:             text,
		Sections:         []distill.Section{{Title: "Intro", Summary: "summary"}},
		Agenda:           []string{"Topic A", "Topic B", "Topic C"},
		Slides: []distill.Slide{
			{Title: "Topic A", Content: "Point 1\nPoint 2"},
			{Title: "Topic B", Content: "Point 1"},
			{Title: "Topic C", Content: "Point 1\nPoint 2"},
		},
	}
	if err := distill.Save(filepath.Join(outDir, sourceID+"_context.json"), dc); err != nil {
		t.Fatal(err)
	}
}

// assertSlidesOutput fails t unless outputFile (relative to dir) exists. A no-op when outputFile
// is empty or wantErr is true — a command that errored isn't expected to have produced output.
// Doesn't check slide count: the stub chunk-outline LLM returns a fixed entry count regardless of
// any requested range or the real per-chunk target embedded in the actual prompt (see
// generateChunkOutline's doc comment), so an exact count from these command-layer tests isn't a
// meaningful signal — see internal/distill's TestAutoSlideRange for precise range-arithmetic
// coverage instead.
func assertSlidesOutput(t *testing.T, dir, outputFile string, wantErr bool) {
	t.Helper()
	if outputFile == "" || wantErr {
		return
	}
	if _, statErr := os.Stat(filepath.Join(dir, outputFile)); statErr != nil {
		t.Fatalf("expected slides output %q: %v", outputFile, statErr)
	}
}

// slidesAutoScalingPrepare returns a TestSlidesCmdRun_Table prepare func for a single-source
// config whose src01 context has an 8000-char (20 * charsPerContentSlide) Text field, giving a
// known distill.AutoSlideRange result (minSlides=19, maxSlides=27) to test resolveSlideRange's
// auto-scaling and clamp behavior against.
func slidesAutoScalingPrepare(minSlides, maxSlides int) func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
	return func(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
		t.Helper()
		pdfPath := filepath.Join(dir, "src.pdf")
		if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfgPath := writeConfigFile(t, dir, pdfPath)
		writeDistilledContextFileWithText(t, dir, "src01", strings.Repeat("x", 8000))
		return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: minSlides, MaxSlides: maxSlides}, &commands.CLI{Config: cfgPath}
	}
}

// moduleAutoScalingPrepare is slidesAutoScalingPrepare's ModuleCmd counterpart: AutoSlideRange
// sums Text length across a module's chapters, so this uses two 4000-char chapter texts
// (src01 + src02) summing to the same 8000 chars, and the same known auto range.
func moduleAutoScalingPrepare(minSlides, maxSlides int) func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
	return func(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
		t.Helper()
		pdfPath := filepath.Join(dir, "src.pdf")
		if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfgPath := writeModuleConfigFile(t, dir, pdfPath)
		writeDistilledContextFileWithText(t, dir, "src01", strings.Repeat("x", 4000))
		writeDistilledContextFileWithText(t, dir, "src02", strings.Repeat("x", 4000))
		return commands.ModuleCmd{ID: "mod1", MinSlides: minSlides, MaxSlides: maxSlides}, &commands.CLI{Config: cfgPath}
	}
}

// slidesZeroSectionsPrepare is a TestSlidesCmdRun_Table prepare func for a single-source config
// whose src01 context has no Sections at all, exercising generateOutlineChunked's pseudo-section
// fallback end-to-end through the CLI rather than failing.
func slidesZeroSectionsPrepare(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
	t.Helper()
	pdfPath := filepath.Join(dir, "src.pdf")
	if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFileWithSections(t, dir, "src01", nil)
	return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
}

// slidesMultiSectionsPrepare is a TestSlidesCmdRun_Table prepare func for a single-source config
// whose src01 context has 3 Sections, proving DistilledContext.Sections ->
// ProtoChapterInput.Sections wiring at the command layer, not just inside the distill package's
// own tests.
func slidesMultiSectionsPrepare(t *testing.T, dir string) (commands.SlidesCmd, *commands.CLI) {
	t.Helper()
	pdfPath := filepath.Join(dir, "src.pdf")
	if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFileWithSections(t, dir, "src01", []distill.Section{
		{Title: "Intro", Summary: "s1"},
		{Title: "Middle", Summary: "s2"},
		{Title: "End", Summary: "s3"},
	})
	return commands.SlidesCmd{IDs: []string{"src01"}, MinSlides: 3, MaxSlides: 20}, &commands.CLI{Config: cfgPath}
}

// moduleZeroSectionsPrepare is slidesZeroSectionsPrepare's ModuleCmd counterpart, spanning two
// sources (src01 + src02) both with no Sections.
func moduleZeroSectionsPrepare(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
	t.Helper()
	pdfPath := filepath.Join(dir, "src.pdf")
	if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeModuleConfigFile(t, dir, pdfPath)
	writeDistilledContextFileWithSections(t, dir, "src01", nil)
	writeDistilledContextFileWithSections(t, dir, "src02", nil)
	return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 8}, &commands.CLI{Config: cfgPath}
}

// moduleMultiSectionsPrepare is slidesMultiSectionsPrepare's ModuleCmd counterpart, spanning two
// sources (src01 with 2 Sections, src02 with 1) to also prove cross-chapter wiring.
func moduleMultiSectionsPrepare(t *testing.T, dir string) (commands.ModuleCmd, *commands.CLI) {
	t.Helper()
	pdfPath := filepath.Join(dir, "src.pdf")
	if err := os.WriteFile(pdfPath, []byte("fake pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeModuleConfigFile(t, dir, pdfPath)
	writeDistilledContextFileWithSections(t, dir, "src01", []distill.Section{
		{Title: "Intro", Summary: "s1"},
		{Title: "Middle", Summary: "s2"},
	})
	writeDistilledContextFileWithSections(t, dir, "src02", []distill.Section{
		{Title: "Advanced", Summary: "s3"},
	})
	return commands.ModuleCmd{ID: "mod1", MinSlides: 3, MaxSlides: 20}, &commands.CLI{Config: cfgPath}
}

// writeSlidesMarkdownFile writes a proto-deck slide Markdown file to <dir>/src01_slides.md whose
// agenda/slides match writeDistilledContextFileWithID's Agenda/Slides fields, and returns its
// path.
func writeSlidesMarkdownFile(t *testing.T, dir string) string {
	t.Helper()
	const md = `# Module 1

---

<!-- meta: 1 agenda -->
# Agenda

- Topic A
- Topic B
- Topic C

---

<!-- meta: 2 src01 -->
# Topic A

- Point 1
- Point 2

---

<!-- meta: 3 src01 -->
# Topic B

- Point 1

---

<!-- meta: 4 src01 -->
# Topic C

- Point 1
- Point 2
`
	path := filepath.Join(dir, "src01_slides.md")
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeModuleConfigFile writes a two-source config grouped into one module "mod1" and returns
// the config path.
func writeModuleConfigFile(t *testing.T, dir, pdfPath string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "quiz.json")
	cfgJSON := fmt.Sprintf(`{"version":1,"defaults":{"workflow":{"outDir":%q}},"sources":[`+
		`{"id":"src01","name":"Chapter 1","chapter":1,"pdf":%q},`+
		`{"id":"src02","name":"Chapter 2","chapter":2,"pdf":%q}],`+
		`"modules":[{"id":"mod1","name":"Module 1","sourceIds":["src01","src02"]}]}`,
		dir, pdfPath, pdfPath)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func handleCanvasMockSuccessRequest(t *testing.T, w http.ResponseWriter, r *http.Request, pagePostCount *int) bool {
	t.Helper()
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages" {
		(*pagePostCount)++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		title := r.Form.Get("wiki_page[title]")
		resp := map[string]any{"page_id": *pagePostCount, "url": fmt.Sprintf("page-%d", *pagePostCount), "title": title}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "name": "Module 1"})
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items" {
		w.WriteHeader(http.StatusCreated)
		return true
	}
	return false
}
