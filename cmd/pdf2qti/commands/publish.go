package commands

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/canvas"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/page"
	"github.com/jh125486/pdf2qti/internal/render"
)

// PublishCmd renders module pages and publishes them to Canvas.
type PublishCmd struct {
	CourseID                    string            `help:"Canvas course ID."                                    name:"course-id"                                                required:""`
	CanvasBaseURL               string            `env:"CANVAS_BASE_URL"                                       help:"Canvas base URL (e.g. https://school.instructure.com)."   name:"canvas-base-url"`
	CanvasToken                 string            `env:"CANVAS_TOKEN"                                          help:"Canvas API token."                                        name:"canvas-token"`
	LearningObjectivesTemplate  string            `help:"Path to learning objectives HTML template."           name:"learning-objectives-template"                             required:""`
	MaterialsTemplate           string            `help:"Path to materials HTML template."                     name:"materials-template"                                       required:""`
	LearningObjectivesTitleTmpl string            `default:"{{.module_name}} Learning Objectives"              help:"Template for learning objectives page title."             name:"learning-objectives-title-template"`
	MaterialsTitleTmpl          string            `default:"{{.module_name}} Materials"                        help:"Template for materials page title."                       name:"materials-title-template"`
	Published                   bool              `default:"true"                                              help:"Publish pages and modules in Canvas."                     name:"published"`
	DryRun                      bool              `help:"Render and log pages without calling the Canvas API." name:"dry-run"`
	Vars                        map[string]string `help:"Extra template vars as key=value pairs."              mapsep:";"                                                      short:"v"`
	IDs                         []string          `arg:""                                                      help:"Optional source IDs to publish. Defaults to all sources." optional:""`
}

type canvasPublisher interface {
	UpsertPage(ctx context.Context, courseID, title, body string, published bool) (*canvas.Page, error)
	EnsureModule(ctx context.Context, courseID, moduleName string, published bool) (*canvas.Module, error)
	EnsureModulePageItem(ctx context.Context, courseID string, moduleID int, pageURL string, published bool) error
}

// Run executes the publish command.
func (p *PublishCmd) Run(ctx context.Context, cli *CLI) error {
	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := audit.New(logOutput)

	sources := p.selectSources(cfg)
	if len(sources) == 0 {
		return fmt.Errorf("no sources selected; specify source IDs or remove IDs to publish all")
	}

	var publisher canvasPublisher
	if !p.DryRun {
		client, err := canvas.NewClient(p.CanvasBaseURL, p.CanvasToken, nil)
		if err != nil {
			return fmt.Errorf("create canvas client: %w", err)
		}
		publisher = client
	}

	for _, src := range sources {
		if err := runPublishSource(ctx, cfg, src, logger, p, publisher, p.DryRun); err != nil {
			return fmt.Errorf("source %q: %w", src.ID, err)
		}
	}
	return nil
}

func (p *PublishCmd) selectSources(cfg *config.Config) []*config.Source {
	if len(p.IDs) == 0 {
		ptrs := make([]*config.Source, len(cfg.Sources))
		for i := range cfg.Sources {
			ptrs[i] = &cfg.Sources[i]
		}
		return ptrs
	}
	idSet := make(map[string]bool, len(p.IDs))
	for _, id := range p.IDs {
		idSet[id] = true
	}
	var ptrs []*config.Source
	for i := range cfg.Sources {
		if idSet[cfg.Sources[i].ID] {
			ptrs = append(ptrs, &cfg.Sources[i])
		}
	}
	return ptrs
}

func runPublishSource(ctx context.Context, cfg *config.Config, src *config.Source, logger *audit.Logger, cmd *PublishCmd, publisher canvasPublisher, dryRun bool) error {
	ctxFile := filepath.Join(cfg.OutDir(src), src.ID+"_context.json")
	dc, err := distill.Load(ctxFile)
	if err != nil {
		return fmt.Errorf("load context %q: %w", ctxFile, err)
	}

	loHTML, err := renderTemplate(cmd.LearningObjectivesTemplate, dc, cmd.Vars)
	if err != nil {
		return fmt.Errorf("render learning objectives HTML: %w", err)
	}
	materialsHTML, err := renderTemplate(cmd.MaterialsTemplate, dc, cmd.Vars)
	if err != nil {
		return fmt.Errorf("render materials HTML: %w", err)
	}

	data := publishTemplateData(dc, src, cmd.Vars)
	loTitle, err := render.ExecuteTemplate(cmd.LearningObjectivesTitleTmpl, data)
	if err != nil {
		return fmt.Errorf("render learning objectives title: %w", err)
	}
	materialsTitle, err := render.ExecuteTemplate(cmd.MaterialsTitleTmpl, data)
	if err != nil {
		return fmt.Errorf("render materials title: %w", err)
	}

	moduleName := firstNonEmpty(dc.ModuleName, src.Name, src.ID)
	if dryRun {
		logger.Info("dry-run publish", "source", src.ID, "module", moduleName, "learningObjectivesTitle", loTitle, "materialsTitle", materialsTitle)
		return nil
	}
	if publisher == nil {
		return fmt.Errorf("publisher is required when dry-run is disabled")
	}

	loPage, err := publisher.UpsertPage(ctx, cmd.CourseID, loTitle, loHTML, cmd.Published)
	if err != nil {
		return fmt.Errorf("publish learning objectives page: %w", err)
	}
	materialsPage, err := publisher.UpsertPage(ctx, cmd.CourseID, materialsTitle, materialsHTML, cmd.Published)
	if err != nil {
		return fmt.Errorf("publish materials page: %w", err)
	}

	module, err := publisher.EnsureModule(ctx, cmd.CourseID, moduleName, cmd.Published)
	if err != nil {
		return fmt.Errorf("ensure module %q: %w", moduleName, err)
	}
	if err := publisher.EnsureModulePageItem(ctx, cmd.CourseID, module.ID, loPage.URL, cmd.Published); err != nil {
		return fmt.Errorf("attach learning objectives page to module: %w", err)
	}
	if err := publisher.EnsureModulePageItem(ctx, cmd.CourseID, module.ID, materialsPage.URL, cmd.Published); err != nil {
		return fmt.Errorf("attach materials page to module: %w", err)
	}

	logger.Info("published module pages", "source", src.ID, "module", moduleName, "learningObjectivesPageURL", loPage.URL, "materialsPageURL", materialsPage.URL)
	return nil
}

func renderTemplate(templatePath string, dc *distill.DistilledContext, vars map[string]string) (string, error) {
	var buf bytes.Buffer
	if err := page.Render(templatePath, dc, vars, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func publishTemplateData(dc *distill.DistilledContext, src *config.Source, vars map[string]string) map[string]any {
	data := map[string]any{
		"source_id":         src.ID,
		"book":              dc.Book,
		"chapter":           dc.Chapter,
		"module_name":       firstNonEmpty(dc.ModuleName, src.Name, src.ID),
		"overview":          dc.Overview,
		"key_concepts":      dc.KeyConcepts,
		"material_overview": dc.MaterialOverview,
		"teaching_notes":    dc.TeachingNotes,
		"objectives":        dc.Objectives,
	}
	for k, v := range vars {
		data[k] = v
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
