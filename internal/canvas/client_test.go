package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNewClient_Validation(t *testing.T) {
	t.Parallel()

	_, err := NewClient("", "token", nil)
	if err == nil {
		t.Fatal("expected error for empty baseURL")
	}

	_, err = NewClient("https://example.instructure.com", "", nil)
	if err == nil {
		t.Fatal("expected error for empty token")
	}

	_, err = NewClient("example.instructure.com", "token", nil)
	if err == nil {
		t.Fatal("expected error for URL without scheme")
	}

	c, err := NewClient("https://example.instructure.com/", "token", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.baseURL != "https://example.instructure.com" {
		t.Fatalf("expected trimmed baseURL, got %q", c.baseURL)
	}
}

func TestUpsertPage_CreateWhenMissing(t *testing.T) {
	t.Parallel()

	var requests []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token" {
			t.Fatalf("missing auth header: %q", auth)
		}
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
			if got := r.URL.Query().Get("search_term"); got != "Learning Objectives" {
				t.Fatalf("unexpected search_term: %q", got)
			}
			writeJSON(t, w, http.StatusOK, []Page{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("wiki_page[title]"); got != "Learning Objectives" {
				t.Fatalf("unexpected title: %q", got)
			}
			if got := r.Form.Get("wiki_page[published]"); got != "true" {
				t.Fatalf("unexpected published: %q", got)
			}
			writeJSON(t, w, http.StatusCreated, Page{PageID: 1001, URL: "learning-objectives", Title: "Learning Objectives"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL, "token", ts.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	page, err := client.UpsertPage(context.Background(), "42", "Learning Objectives", "<h1>Body</h1>", true)
	if err != nil {
		t.Fatalf("upsert page: %v", err)
	}
	if page.PageID != 1001 || page.URL != "learning-objectives" {
		t.Fatalf("unexpected page response: %+v", page)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
}

func TestUpsertPage_UpdateWhenExisting(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
			writeJSON(t, w, http.StatusOK, []Page{{PageID: 77, Title: "Learning Objectives", URL: "learning-objectives"}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/courses/42/pages/77":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("wiki_page[body]"); got != "<p>Updated</p>" {
				t.Fatalf("unexpected body: %q", got)
			}
			writeJSON(t, w, http.StatusOK, Page{PageID: 77, URL: "learning-objectives", Title: "Learning Objectives"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL, "token", ts.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	page, err := client.UpsertPage(context.Background(), "42", "Learning Objectives", "<p>Updated</p>", false)
	if err != nil {
		t.Fatalf("upsert page: %v", err)
	}
	if page.PageID != 77 {
		t.Fatalf("unexpected page id: %d", page.PageID)
	}
}

func TestEnsureModule_ReturnsExisting(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/courses/42/modules" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusOK, []Module{{ID: 9, Name: "Module 1"}})
	}))
	defer ts.Close()

	client, _ := NewClient(ts.URL, "token", ts.Client())
	mod, err := client.EnsureModule(context.Background(), "42", "Module 1", true)
	if err != nil {
		t.Fatalf("ensure module: %v", err)
	}
	if mod.ID != 9 {
		t.Fatalf("expected module id 9, got %d", mod.ID)
	}
}

func TestEnsureModule_CreatesWhenMissing(t *testing.T) {
	t.Parallel()
	var sawPost bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules":
			writeJSON(t, w, http.StatusOK, []Module{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules":
			sawPost = true
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("module[name]"); got != "Module 2" {
				t.Fatalf("unexpected module name: %q", got)
			}
			writeJSON(t, w, http.StatusCreated, Module{ID: 22, Name: "Module 2"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	client, _ := NewClient(ts.URL, "token", ts.Client())
	mod, err := client.EnsureModule(context.Background(), "42", "Module 2", true)
	if err != nil {
		t.Fatalf("ensure module: %v", err)
	}
	if !sawPost {
		t.Fatal("expected module create POST")
	}
	if mod.ID != 22 {
		t.Fatalf("expected module id 22, got %d", mod.ID)
	}
}

func TestEnsureModulePageItem_NoopWhenExists(t *testing.T) {
	t.Parallel()
	var postCount int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			writeJSON(t, w, http.StatusOK, []ModuleItem{{Type: "Page", PageURL: "materials-page"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			postCount++
			writeJSON(t, w, http.StatusCreated, map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	client, _ := NewClient(ts.URL, "token", ts.Client())
	err := client.EnsureModulePageItem(context.Background(), "42", 7, "materials-page", true)
	if err != nil {
		t.Fatalf("ensure module item: %v", err)
	}
	if postCount != 0 {
		t.Fatalf("expected no POST when item exists, got %d", postCount)
	}
}

func TestEnsureModulePageItem_CreatesWhenMissing(t *testing.T) {
	t.Parallel()
	var createdType string
	var createdPageURL string
	var createdPublished string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			writeJSON(t, w, http.StatusOK, []ModuleItem{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			createdType = r.Form.Get("module_item[type]")
			createdPageURL = r.Form.Get("module_item[page_url]")
			createdPublished = r.Form.Get("module_item[published]")
			writeJSON(t, w, http.StatusCreated, map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	client, _ := NewClient(ts.URL, "token", ts.Client())
	err := client.EnsureModulePageItem(context.Background(), "42", 7, "learning-objectives", false)
	if err != nil {
		t.Fatalf("ensure module item: %v", err)
	}
	if createdType != "Page" {
		t.Fatalf("unexpected module_item[type]: %q", createdType)
	}
	if createdPageURL != "learning-objectives" {
		t.Fatalf("unexpected module_item[page_url]: %q", createdPageURL)
	}
	if createdPublished != "false" {
		t.Fatalf("unexpected module_item[published]: %q", createdPublished)
	}
}

func TestRequestJSON_StatusErrorIncludesPayload(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request payload"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL, "token", ts.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	err = client.requestJSON(context.Background(), http.MethodGet, "/api/v1/courses/42/pages", nil, nil, nil, http.StatusOK)
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(http.StatusBadRequest)) {
		t.Fatalf("expected status code in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad request payload") {
		t.Fatalf("expected payload in error, got: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func TestRequestJSON_DecodeError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not-json")
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL, "token", ts.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	var out map[string]any
	err = client.requestJSON(context.Background(), http.MethodGet, "/api/v1/courses/42/modules", nil, nil, &out, http.StatusOK)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("unexpected error: %v", err)
	}
}
