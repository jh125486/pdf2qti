package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jh125486/pdf2qti/internal/canvas"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

type upsertCall struct {
	title string
	body  string
}

type moduleItemCall struct {
	moduleID int
	pageURL  string
}

type fakeCanvasPublisher struct {
	upserts       []upsertCall
	ensuredModule []string
	moduleItems   []moduleItemCall
}

func (f *fakeCanvasPublisher) UpsertPage(_ context.Context, _, title, body string, _ bool) (*canvas.Page, error) {
	f.upserts = append(f.upserts, upsertCall{title: title, body: body})
	return &canvas.Page{PageID: len(f.upserts), URL: fmt.Sprintf("page-%d", len(f.upserts)), Title: title}, nil
}

func (f *fakeCanvasPublisher) EnsureModule(_ context.Context, _, moduleName string, _ bool) (*canvas.Module, error) {
	f.ensuredModule = append(f.ensuredModule, moduleName)
	return &canvas.Module{ID: 17, Name: moduleName}, nil
}

func (f *fakeCanvasPublisher) EnsureModulePageItem(_ context.Context, _ string, moduleID int, pageURL string, _ bool) error {
	f.moduleItems = append(f.moduleItems, moduleItemCall{moduleID: moduleID, pageURL: pageURL})
	return nil
}

func writePublishContext(t *testing.T, outDir string) {
	t.Helper()
	dc := &distill.DistilledContext{
		SourceID:         "src01",
		Book:             "Systems Programming",
		Chapter:          3,
		ModuleName:       "Module 3",
		Overview:         "<p>Overview HTML</p>",
		KeyConcepts:      []string{"fork", "exec"},
		MaterialOverview: "Read chapter and slides",
		Objectives: []distill.Objective{
			{CO: 1, Text: "Explain process creation"},
		},
	}
	if err := distill.Save(filepath.Join(outDir, "src01_context.json"), dc); err != nil {
		t.Fatal(err)
	}
}

func writeTemplateFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunPublishSource_PublishesTwoPagesAndModuleItems(t *testing.T) {
	dir := t.TempDir()
	writePublishContext(t, dir)
	loTemplate := writeTemplateFile(t, dir, "learning_objectives.html.tmpl", "<h2>{{.module_name}}</h2>{{.overview}}")
	materialsTemplate := writeTemplateFile(t, dir, "materials.html.tmpl", "<p>{{.material_overview}}</p>")

	cfg := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{OutDir: dir},
		},
		Sources: []config.Source{{ID: "src01", Name: "Source One"}},
	}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{
		CourseID:                    "42",
		LearningObjectivesTemplate:  loTemplate,
		MaterialsTemplate:           materialsTemplate,
		LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
		MaterialsTitleTmpl:          "{{.module_name}} Materials",
		Published:                   true,
	}
	pub := &fakeCanvasPublisher{}

	if err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, pub, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.upserts) != 2 {
		t.Fatalf("expected 2 upsert calls, got %d", len(pub.upserts))
	}
	if pub.upserts[0].title != "Module 3 Learning Objectives" {
		t.Fatalf("unexpected learning objectives title: %q", pub.upserts[0].title)
	}
	if pub.upserts[1].title != "Module 3 Materials" {
		t.Fatalf("unexpected materials title: %q", pub.upserts[1].title)
	}
	if len(pub.ensuredModule) != 1 || pub.ensuredModule[0] != "Module 3" {
		t.Fatalf("expected module ensure call for Module 3, got %v", pub.ensuredModule)
	}
	if len(pub.moduleItems) != 2 {
		t.Fatalf("expected 2 module item calls, got %d", len(pub.moduleItems))
	}
}

func TestRunPublishSource_DryRunSkipsCanvasCalls(t *testing.T) {
	dir := t.TempDir()
	writePublishContext(t, dir)
	loTemplate := writeTemplateFile(t, dir, "learning_objectives.html.tmpl", "<h2>{{.module_name}}</h2>")
	materialsTemplate := writeTemplateFile(t, dir, "materials.html.tmpl", "<p>{{.material_overview}}</p>")

	cfg := &config.Config{
		Version: 1,
		Defaults: config.Defaults{
			Workflow: config.Workflow{OutDir: dir},
		},
		Sources: []config.Source{{ID: "src01", Name: "Source One"}},
	}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{
		CourseID:                    "42",
		LearningObjectivesTemplate:  loTemplate,
		MaterialsTemplate:           materialsTemplate,
		LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
		MaterialsTitleTmpl:          "{{.module_name}} Materials",
		Published:                   true,
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	if err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, nil, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
