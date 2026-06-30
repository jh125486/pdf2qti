package commands

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

func TestPublishCmdRun_SuccessDryRun(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFile(t, dir, "src01")

	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &PublishCmd{
		CourseID:                    "42",
		DryRun:                      true,
		LearningObjectivesTemplate:  loTemplate,
		MaterialsTemplate:           materialsTemplate,
		LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
		MaterialsTitleTmpl:          "{{.module_name}} Materials",
	}
	if err := cmd.Run(context.Background(), &CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishCmdRun_NoSelectedSources(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)

	cmd := &PublishCmd{IDs: []string{"nope"}, DryRun: true}
	err := cmd.Run(context.Background(), &CLI{Config: cfgPath})
	if err == nil {
		t.Fatal("expected no selected sources error")
	}
}

func TestPublishCmdRun_ClientCreationError(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)

	cmd := &PublishCmd{DryRun: false, CanvasBaseURL: "", CanvasToken: "token"}
	err := cmd.Run(context.Background(), &CLI{Config: cfgPath})
	if err == nil {
		t.Fatal("expected canvas client creation error")
	}
}

func TestPublishHelpers(t *testing.T) {
	dc := &distill.DistilledContext{ModuleName: "Module A", Book: "Book", Chapter: 2, MaterialOverview: "overview"}
	src := &config.Source{ID: "src01", Name: "Source 1"}

	data := publishTemplateData(dc, src, map[string]string{moduleNameTemplateKey: "Override"})
	if got := data[moduleNameTemplateKey]; got != "Override" {
		t.Fatalf("expected override module name, got %v", got)
	}

	if got := firstNonEmpty("", "x", "y"); got != "x" {
		t.Fatalf("unexpected first non-empty value: %q", got)
	}

	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("expected empty result, got %q", got)
	}
}

func TestRenderTemplate_Error(t *testing.T) {
	_, err := renderTemplate("/does/not/exist.tmpl", &distill.DistilledContext{}, nil)
	if err == nil {
		t.Fatal("expected render template error")
	}
}

func TestRunPublishSource_PublisherRequiredWhenNotDryRun(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{CourseID: "42", LearningObjectivesTemplate: loTemplate, MaterialsTemplate: materialsTemplate, LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives", MaterialsTitleTmpl: "{{.module_name}} Materials"}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, nil, false)
	if err == nil {
		t.Fatal("expected publisher required error")
	}
}

func TestRunPublishSource_MissingContextError(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{LearningObjectivesTemplate: "missing", MaterialsTemplate: "missing"}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, nil, true)
	if err == nil {
		t.Fatal("expected missing context error")
	}
}

func TestRunPublishSource_TitleTemplateError(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{LearningObjectivesTemplate: loTemplate, MaterialsTemplate: materialsTemplate, LearningObjectivesTitleTmpl: "{{call .}}", MaterialsTitleTmpl: "{{.module_name}} Materials"}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, nil, true)
	if err == nil {
		t.Fatal("expected title template execute error")
	}
}

func TestPublishCmdRun_SuccessNonDryRun(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFile(t, dir, "src01")

	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var pagePostCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCanvasMockSuccessRequest(t, w, r, &pagePostCount) {
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &PublishCmd{
		CourseID:                    "42",
		CanvasBaseURL:               server.URL,
		CanvasToken:                 "token",
		DryRun:                      false,
		LearningObjectivesTemplate:  loTemplate,
		MaterialsTemplate:           materialsTemplate,
		LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
		MaterialsTitleTmpl:          "{{.module_name}} Materials",
		Published:                   true,
	}
	if err := cmd.Run(context.Background(), &CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected non-dry-run error: %v", err)
	}
}

func TestPublishCmdSelectSources_AllAndIDs(t *testing.T) {
	cfg := &config.Config{Sources: []config.Source{{ID: "src01"}, {ID: "src02"}}}
	cmdAll := &PublishCmd{}
	gotAll := cmdAll.selectSources(cfg)
	if len(gotAll) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(gotAll))
	}

	cmdIDs := &PublishCmd{IDs: []string{"src02"}}
	gotIDs := cmdIDs.selectSources(cfg)
	wantIDs := []*config.Source{&cfg.Sources[1]}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("unexpected sources for IDs: got=%+v want=%+v", gotIDs, wantIDs)
	}
}

func TestRunPublishSource_UpsertError(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{CourseID: "42", LearningObjectivesTemplate: loTemplate, MaterialsTemplate: materialsTemplate, LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives", MaterialsTitleTmpl: "{{.module_name}} Materials", Published: true}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, &failingPublisher{failAt: "upsert"}, false)
	if err == nil {
		t.Fatal("expected upsert error")
	}
}

func TestRunPublishSource_EnsureModuleError(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{CourseID: "42", LearningObjectivesTemplate: loTemplate, MaterialsTemplate: materialsTemplate, LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives", MaterialsTitleTmpl: "{{.module_name}} Materials", Published: true}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, &failingPublisher{failAt: "module"}, false)
	if err == nil {
		t.Fatal("expected ensure module error")
	}
}

func TestRunPublishSource_AttachError(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}}, Sources: []config.Source{{ID: "src01", Name: "Source 1"}}}
	src := &cfg.Sources[0]
	cmd := &PublishCmd{CourseID: "42", LearningObjectivesTemplate: loTemplate, MaterialsTemplate: materialsTemplate, LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives", MaterialsTitleTmpl: "{{.module_name}} Materials", Published: true}

	err := runPublishSource(context.Background(), cfg, src, silentLogger(), cmd, &failingPublisher{failAt: "attach"}, false)
	if err == nil {
		t.Fatal("expected attach error")
	}
}

func TestPublishCmdRun_LoadConfigError(t *testing.T) {
	cmd := &PublishCmd{DryRun: true}
	err := cmd.Run(context.Background(), &CLI{Config: "nonexistent_config.json"})
	if err == nil {
		t.Fatal("expected load config error")
	}
}
