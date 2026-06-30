package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jh125486/pdf2qti/internal/canvas"
	"github.com/jh125486/pdf2qti/internal/distill"
)

type badLLM struct{}

func (b *badLLM) Complete(_ context.Context, _ string) (string, error) {
	return "{", nil
}

type failingPublisher struct {
	failAt string
}

func (f *failingPublisher) UpsertPage(_ context.Context, _, _, _ string, _ bool) (*canvas.Page, error) {
	if f.failAt == "upsert" {
		return nil, errors.New("upsert failed")
	}
	return &canvas.Page{PageID: 1, URL: "page-1", Title: "title"}, nil
}

func (f *failingPublisher) EnsureModule(_ context.Context, _, _ string, _ bool) (*canvas.Module, error) {
	if f.failAt == "module" {
		return nil, errors.New("module failed")
	}
	return &canvas.Module{ID: 7, Name: "Module"}, nil
}

func (f *failingPublisher) EnsureModulePageItem(_ context.Context, _ string, _ int, _ string, _ bool) error {
	if f.failAt == "attach" {
		return errors.New("attach failed")
	}
	return nil
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

func writeDistilledContextFile(t *testing.T, outDir, sourceID string) {
	t.Helper()
	dc := &distill.DistilledContext{
		SourceID:         sourceID,
		Book:             "Book",
		Chapter:          1,
		ModuleName:       "Module 1",
		Overview:         "<p>Overview</p>",
		MaterialOverview: "Read this",
		KeyConcepts:      []string{"pipes"},
	}
	if err := distill.Save(filepath.Join(outDir, sourceID+"_context.json"), dc); err != nil {
		t.Fatal(err)
	}
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
