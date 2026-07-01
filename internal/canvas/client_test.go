package canvas_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/canvas"
)

func TestNewClient_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		token   string
		wantErr bool
	}{
		{name: "empty base url", baseURL: "", token: "token", wantErr: true},
		{name: "empty token", baseURL: "https://example.instructure.com", token: "", wantErr: true},
		{name: "missing scheme", baseURL: "example.instructure.com", token: "token", wantErr: true},
		{name: "valid", baseURL: "https://example.instructure.com/", token: " token\n"},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := canvas.NewClient(tt.baseURL, tt.token, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && client == nil {
				t.Fatal("expected non-nil client")
			}
		})
	}
}

func TestUpsertPage_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server func(t *testing.T) (*httptest.Server, *int)
		wantID int
	}{
		{
			name:   "create when missing",
			server: setupCreatePageServer,
			wantID: 1001,
		},
		{
			name:   "update when existing",
			server: setupUpdatePageServer,
			wantID: 77,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts, _ := tt.server(t)
			defer ts.Close()

			client, err := canvas.NewClient(ts.URL, "token", ts.Client())
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			page, err := client.UpsertPage(context.Background(), "42", "Learning Objectives", "<h1>Body</h1>", true)
			if err != nil {
				t.Fatalf("upsert page: %v", err)
			}
			if page.PageID != tt.wantID {
				t.Fatalf("page id=%d want=%d", page.PageID, tt.wantID)
			}
		})
	}
}

func setupCreatePageServer(t *testing.T) (ts *httptest.Server, requestCount *int) {
	t.Helper()
	requests := 0
	requestCount = &requests
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token" {
			t.Fatalf("unexpected auth header: %q", auth)
		}
		requests++
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
			if got := r.URL.Query().Get("search_term"); got != "Learning Objectives" {
				t.Fatalf("expected search_term=%q, got %q", "Learning Objectives", got)
			}
			writeJSON(t, w, http.StatusOK, []canvas.Page{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages":
			assertWikiPageForm(t, r, "Learning Objectives", "<h1>Body</h1>")
			if got := r.Form.Get("wiki_page[published]"); got != "true" {
				t.Fatalf("expected published=true, got %q", got)
			}
			writeJSON(t, w, http.StatusCreated, canvas.Page{PageID: 1001, URL: "learning-objectives", Title: "Learning Objectives"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	return ts, requestCount
}

func setupUpdatePageServer(t *testing.T) (ts *httptest.Server, requestCount *int) {
	t.Helper()
	requests := 0
	requestCount = &requests
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
			writeJSON(t, w, http.StatusOK, []canvas.Page{{PageID: 77, Title: "Learning Objectives", URL: "learning-objectives"}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/courses/42/pages/77":
			assertWikiPageForm(t, r, "Learning Objectives", "<h1>Body</h1>")
			writeJSON(t, w, http.StatusOK, canvas.Page{PageID: 77, URL: "learning-objectives", Title: "Learning Objectives"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	return ts, requestCount
}

// assertWikiPageForm parses the request form and asserts the wiki_page title/body fields.
func assertWikiPageForm(t *testing.T, r *http.Request, wantTitle, wantBody string) {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	if got := r.Form.Get("wiki_page[title]"); got != wantTitle {
		t.Fatalf("expected title=%q, got %q", wantTitle, got)
	}
	if got := r.Form.Get("wiki_page[body]"); got != wantBody {
		t.Fatalf("expected body=%q, got %q", wantBody, got)
	}
}

func TestUpsertPage_RequestErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		server  func(t *testing.T) *httptest.Server
		errLike string
	}{
		{
			name: "unexpected status includes payload",
			server: func(t *testing.T) *httptest.Server {
				t.Helper()
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
						writeJSON(t, w, http.StatusOK, []canvas.Page{})
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages":
						w.WriteHeader(http.StatusForbidden)
						_, _ = w.Write([]byte(`{"errors":"not allowed"}`))
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
					}
				}))
			},
			errLike: "not allowed",
		},
		{
			name: "response decode error",
			server: func(t *testing.T) *httptest.Server {
				t.Helper()
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/pages":
						writeJSON(t, w, http.StatusOK, []canvas.Page{})
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages":
						w.WriteHeader(http.StatusCreated)
						_, _ = w.Write([]byte(`not-json`))
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
					}
				}))
			},
			errLike: "decode",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := tt.server(t)
			defer ts.Close()

			client, err := canvas.NewClient(ts.URL, "token", ts.Client())
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			_, err = client.UpsertPage(context.Background(), "42", "Learning Objectives", "<h1>Body</h1>", true)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errLike) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}

func TestEnsureModule_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		moduleName string
		server     func(t *testing.T) *httptest.Server
		wantID     int
	}{
		{
			name:       "returns existing",
			moduleName: "Module 1",
			server: func(t *testing.T) *httptest.Server {
				t.Helper()
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules" {
						writeJSON(t, w, http.StatusOK, []canvas.Module{{ID: 9, Name: "Module 1"}})
						return
					}
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}))
			},
			wantID: 9,
		},
		{
			name:       "creates when missing",
			moduleName: "Module 2",
			server: func(t *testing.T) *httptest.Server {
				t.Helper()
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules":
						writeJSON(t, w, http.StatusOK, []canvas.Module{})
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules":
						writeJSON(t, w, http.StatusCreated, canvas.Module{ID: 22, Name: "Module 2"})
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
					}
				}))
			},
			wantID: 22,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := tt.server(t)
			defer ts.Close()

			client, _ := canvas.NewClient(ts.URL, "token", ts.Client())
			mod, err := client.EnsureModule(context.Background(), "42", tt.moduleName, true)
			if err != nil {
				t.Fatalf("ensure module: %v", err)
			}
			if mod.ID != tt.wantID {
				t.Fatalf("module id=%d want=%d", mod.ID, tt.wantID)
			}
		})
	}
}

func TestEnsureModulePageItem_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(t *testing.T) (*httptest.Server, *int)
		wantPost int
	}{
		{
			name: "noop when exists",
			setup: func(t *testing.T) (*httptest.Server, *int) {
				t.Helper()
				postCount := 0
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items":
						writeJSON(t, w, http.StatusOK, []canvas.ModuleItem{{Type: "Page", PageURL: "materials-page"}})
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items":
						postCount++
						writeJSON(t, w, http.StatusCreated, map[string]any{"ok": true})
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
					}
				}))
				return ts, &postCount
			},
			wantPost: 0,
		},
		{
			name: "create when missing",
			setup: func(t *testing.T) (*httptest.Server, *int) {
				t.Helper()
				postCount := 0
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items":
						writeJSON(t, w, http.StatusOK, []canvas.ModuleItem{})
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items":
						postCount++
						writeJSON(t, w, http.StatusCreated, map[string]any{"ok": true})
					default:
						t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
					}
				}))
				return ts, &postCount
			},
			wantPost: 1,
		},
		{
			name:     "paginated listing finds item on second page",
			setup:    setupPaginatedModuleItemsServer,
			wantPost: 0,
		},
		{
			name:     "paginated listing follows unquoted rel=next Link header",
			setup:    setupPaginatedModuleItemsServerWithLinkRel,
			wantPost: 0,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts, postCount := tt.setup(t)
			defer ts.Close()

			client, _ := canvas.NewClient(ts.URL, "token", ts.Client())
			err := client.EnsureModulePageItem(context.Background(), "42", 7, "materials-page", true)
			if err != nil {
				t.Fatalf("ensure module page item: %v", err)
			}
			if *postCount != tt.wantPost {
				t.Fatalf("post count=%d want=%d", *postCount, tt.wantPost)
			}
		})
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

func setupPaginatedModuleItemsServer(t *testing.T) (ts *httptest.Server, postCount *int) {
	t.Helper()
	return newPaginatedModuleItemsServer(t, `rel="next"`)
}

func setupPaginatedModuleItemsServerWithLinkRel(t *testing.T) (ts *httptest.Server, postCount *int) {
	t.Helper()
	return newPaginatedModuleItemsServer(t, `rel=next`)
}

func newPaginatedModuleItemsServer(t *testing.T, linkRel string) (ts *httptest.Server, postCount *int) {
	t.Helper()
	var count int
	postCount = &count
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://example.instructure.com/api/v1/courses/42/modules/7/items?page=2>; `+linkRel)
				writeJSON(t, w, http.StatusOK, []canvas.ModuleItem{{Type: "Page", PageURL: "other-page"}})
				return
			}
			writeJSON(t, w, http.StatusOK, []canvas.ModuleItem{{Type: "Page", PageURL: "materials-page"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items":
			count++
			writeJSON(t, w, http.StatusCreated, map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	return ts, postCount
}
