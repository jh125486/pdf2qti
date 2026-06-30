package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
)

func TestDistillCmdRun_NoSourcesSelected(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("fake pdf content"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)

	cmd := &DistillCmd{}
	err := cmd.Run(context.Background(), &CLI{Config: cfgPath})
	if err == nil {
		t.Fatal("expected no sources selected error")
	}
}

func TestDistillCmdRun_SuccessAll(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(hello from chapter text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)

	orig := logOutput
	logOutput = io.Discard
	t.Cleanup(func() { logOutput = orig })

	cmd := &DistillCmd{All: true}
	if err := cmd.Run(context.Background(), &CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src01_context.json")); err != nil {
		t.Fatalf("expected context output: %v", err)
	}
}

func TestRunDistillSource_ContextExistsWithoutForce(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Version:  1,
		Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}},
		Sources:  []config.Source{{ID: "src01", Name: "Chapter 1", Chapter: 1, PDF: pdfPath}},
	}
	src := &cfg.Sources[0]
	writeDistilledContextFile(t, dir, src.ID)

	err := runDistillSource(context.Background(), cfg, src, silentLogger(), &stubDistillLLM{}, false)
	if err == nil {
		t.Fatal("expected existing context error")
	}
}

func TestDistillCmdSelectSources(t *testing.T) {
	cfg := &config.Config{Sources: []config.Source{{ID: "a"}, {ID: "b"}}}

	all := (&DistillCmd{All: true}).selectSources(cfg)
	if len(all) != 2 {
		t.Fatalf("expected all sources, got %d", len(all))
	}

	selected := (&DistillCmd{IDs: []string{"b"}}).selectSources(cfg)
	if len(selected) != 1 || selected[0].ID != "b" {
		t.Fatalf("unexpected selected sources: %+v", selected)
	}

	none := (&DistillCmd{}).selectSources(cfg)
	if none != nil {
		t.Fatalf("expected nil when no IDs and not all, got %+v", none)
	}
}

func TestRunDistillSource_ExtractError(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Version:  1,
		Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}},
		Sources:  []config.Source{{ID: "src01", Name: "Chapter 1", Chapter: 1, PDF: filepath.Join(dir, "missing.pdf")}},
	}
	src := &cfg.Sources[0]

	err := runDistillSource(context.Background(), cfg, src, silentLogger(), &stubDistillLLM{}, true)
	if err == nil {
		t.Fatal("expected extract error")
	}
}

func TestRunDistillSource_DistillError(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Version:  1,
		Defaults: config.Defaults{Workflow: config.Workflow{OutDir: dir}},
		Sources:  []config.Source{{ID: "src01", Name: "Chapter 1", Chapter: 1, PDF: pdfPath}},
	}
	src := &cfg.Sources[0]

	err := runDistillSource(context.Background(), cfg, src, silentLogger(), &badLLM{}, true)
	if err == nil {
		t.Fatal("expected distill error")
	}
}

func TestDistillCmdRun_LoadConfigError(t *testing.T) {
	cmd := &DistillCmd{All: true}
	err := cmd.Run(context.Background(), &CLI{Config: "nonexistent_config.json"})
	if err == nil {
		t.Fatal("expected load config error")
	}
}
