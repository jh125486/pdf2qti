package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPageCmdRun_Success(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	templatePath := filepath.Join(dir, "page.tmpl")
	outPath := filepath.Join(dir, "out.html")
	if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := &PageCmd{Context: filepath.Join(dir, "src01_context.json"), Template: templatePath, Output: outPath}
	if err := cmd.Run(context.Background(), &CLI{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}

func TestPageCmdRun_CreateOutputError(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	templatePath := filepath.Join(dir, "page.tmpl")
	if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := &PageCmd{Context: filepath.Join(dir, "src01_context.json"), Template: templatePath, Output: dir}
	err := cmd.Run(context.Background(), &CLI{})
	if err == nil {
		t.Fatal("expected create output error")
	}
}

func TestPageCmdRun_LoadContextError(t *testing.T) {
	cmd := &PageCmd{Context: "/no/context.json", Template: "/no/template.tmpl"}
	err := cmd.Run(context.Background(), &CLI{})
	if err == nil {
		t.Fatal("expected load context error")
	}
}

func TestExecute_PageCommandSuccess(t *testing.T) {
	dir := t.TempDir()
	writeDistilledContextFile(t, dir, "src01")
	templatePath := filepath.Join(dir, "page.tmpl")
	outPath := filepath.Join(dir, "out.html")
	if err := os.WriteFile(templatePath, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{
		"pdf2qti",
		"--config", filepath.Join(dir, "unused.json"),
		"page",
		"--context", filepath.Join(dir, "src01_context.json"),
		"--output", outPath,
		templatePath,
	}

	err := Execute()
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
}
