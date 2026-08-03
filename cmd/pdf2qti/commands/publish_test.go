package commands_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestPublishCmdRun_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI)
		wantErr bool
	}{
		{
			name: "success dry run",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFile(t, dir)

				loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
				materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
				if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
					t.Fatal(err)
				}

				cmd := &commands.PublishCmd{
					CourseID:                    "42",
					DryRun:                      true,
					LearningObjectivesTemplate:  loTemplate,
					MaterialsTemplate:           materialsTemplate,
					LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
					MaterialsTitleTmpl:          "{{.module_name}} Materials",
				}
				return cmd, &commands.CLI{Config: cfgPath}
			},
		},
		{
			name: "no selected sources",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return &commands.PublishCmd{IDs: []string{"nope"}, DryRun: true}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "render learning objectives template error",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFile(t, dir)
				return &commands.PublishCmd{
					CourseID:                   "42",
					DryRun:                     true,
					LearningObjectivesTemplate: filepath.Join(dir, "missing.html.tmpl"),
					MaterialsTemplate:          filepath.Join(dir, "missing.html.tmpl"),
				}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "module name falls back to source id",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := filepath.Join(dir, "quiz.json")
				cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"` + pdfPath + `"}]}`
				if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				dc := map[string]any{"source_id": "src01"}
				b, err := json.Marshal(dc)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "src01_context.json"), b, 0o600); err != nil {
					t.Fatal(err)
				}

				loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
				materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
				if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &commands.PublishCmd{
					CourseID:                    "42",
					DryRun:                      true,
					LearningObjectivesTemplate:  loTemplate,
					MaterialsTemplate:           materialsTemplate,
					LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
					MaterialsTitleTmpl:          "{{.module_name}} Materials",
					Vars:                        map[string]string{"custom": "value"},
				}, &commands.CLI{Config: cfgPath}
			},
		},
		{
			name: "client creation error",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return &commands.PublishCmd{DryRun: false, CanvasBaseURL: "", CanvasToken: "token"}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "load config error",
			prepare: func(_ *testing.T, _ string) (*commands.PublishCmd, *commands.CLI) {
				return &commands.PublishCmd{DryRun: true}, &commands.CLI{Config: "nonexistent_config.json"}
			},
			wantErr: true,
		},
		{
			name: "load context error, missing context file",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				return &commands.PublishCmd{
					CourseID: "42", DryRun: true,
					LearningObjectivesTemplate: filepath.Join(dir, "lo.html.tmpl"),
					MaterialsTemplate:          filepath.Join(dir, "materials.html.tmpl"),
				}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			// LO template is valid (so its own render succeeds), but the materials template is
			// missing — distinct from "render learning objectives template error" above, which
			// fails at the LO render itself before materials is ever reached.
			name: "render materials template error",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFile(t, dir)
				loTemplate := filepath.Join(dir, "lo.html.tmpl")
				if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &commands.PublishCmd{
					CourseID: "42", DryRun: true,
					LearningObjectivesTemplate: loTemplate,
					MaterialsTemplate:          filepath.Join(dir, "missing_materials.html.tmpl"),
				}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "learning objectives title template error",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFile(t, dir)
				loTemplate := filepath.Join(dir, "lo.html.tmpl")
				materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
				if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &commands.PublishCmd{
					CourseID: "42", DryRun: true,
					LearningObjectivesTemplate:  loTemplate,
					MaterialsTemplate:           materialsTemplate,
					LearningObjectivesTitleTmpl: "{{.module_name",
					MaterialsTitleTmpl:          "{{.module_name}} Materials",
				}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
		{
			name: "materials title template error",
			prepare: func(t *testing.T, dir string) (*commands.PublishCmd, *commands.CLI) {
				t.Helper()
				pdfPath := filepath.Join(dir, "src01.pdf")
				if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfgPath := writeConfigFile(t, dir, pdfPath)
				writeDistilledContextFile(t, dir)
				loTemplate := filepath.Join(dir, "lo.html.tmpl")
				materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
				if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &commands.PublishCmd{
					CourseID: "42", DryRun: true,
					LearningObjectivesTemplate:  loTemplate,
					MaterialsTemplate:           materialsTemplate,
					LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
					MaterialsTitleTmpl:          "{{.module_name",
				}, &commands.CLI{Config: cfgPath}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cmd, cli := tt.prepare(t, dir)
			err := cmd.Run(context.Background(), cli)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestExecute_PublishDryRunWithoutCanvasCredentials(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFile(t, dir)

	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	withArgs(t, []string{
		"pdf2qti",
		"--config", cfgPath,
		"publish",
		"--course-id", "42",
		"--dry-run",
		"--learning-objectives-template", loTemplate,
		"--materials-template", materialsTemplate,
	})

	if err := commands.Execute(); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
}

func TestPublishCmdRun_SuccessNonDryRun(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFile(t, dir)

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

	cmd := &commands.PublishCmd{
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
	if err := cmd.Run(context.Background(), &commands.CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected non-dry-run error: %v", err)
	}
}

// publishTestSetup writes a config, distilled context, and LO/materials templates under dir and
// returns a ready-to-run non-dry-run PublishCmd pointed at serverURL.
func publishTestSetup(t *testing.T, dir, serverURL string) (*commands.PublishCmd, string) {
	t.Helper()
	pdfPath := filepath.Join(dir, "src01.pdf")
	if err := os.WriteFile(pdfPath, []byte("(pdf text)"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfigFile(t, dir, pdfPath)
	writeDistilledContextFile(t, dir)

	loTemplate := filepath.Join(dir, "learning_objectives.html.tmpl")
	materialsTemplate := filepath.Join(dir, "materials.html.tmpl")
	if err := os.WriteFile(loTemplate, []byte("<h1>{{.module_name}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialsTemplate, []byte("<p>{{.material_overview}}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}

	return &commands.PublishCmd{
		CourseID:                    "42",
		CanvasBaseURL:               serverURL,
		CanvasToken:                 "token",
		DryRun:                      false,
		LearningObjectivesTemplate:  loTemplate,
		MaterialsTemplate:           materialsTemplate,
		LearningObjectivesTitleTmpl: "{{.module_name}} Learning Objectives",
		MaterialsTitleTmpl:          "{{.module_name}} Materials",
		Published:                   true,
	}, cfgPath
}

func TestPublishCmdRun_CanvasAPIErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		errLike     string
		handlerFail func(w http.ResponseWriter, r *http.Request, pagePostCount *int) bool
	}{
		{
			name:    "learning objectives page upsert fails",
			errLike: "publish learning objectives page",
			handlerFail: func(w http.ResponseWriter, r *http.Request, pagePostCount *int) bool {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages" && *pagePostCount == 0 {
					w.WriteHeader(http.StatusInternalServerError)
					return true
				}
				return false
			},
		},
		{
			name:    "materials page upsert fails",
			errLike: "publish materials page",
			handlerFail: func(w http.ResponseWriter, r *http.Request, pagePostCount *int) bool {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/pages" && *pagePostCount == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					return true
				}
				return false
			},
		},
		{
			name:    "ensure module fails",
			errLike: `ensure module`,
			handlerFail: func(w http.ResponseWriter, r *http.Request, _ *int) bool {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules" {
					w.WriteHeader(http.StatusInternalServerError)
					return true
				}
				return false
			},
		},
		{
			name:    "attach page to module fails",
			errLike: "attach learning objectives page to module",
			handlerFail: func(w http.ResponseWriter, r *http.Request, _ *int) bool {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/courses/42/modules/7/items" {
					w.WriteHeader(http.StatusInternalServerError)
					return true
				}
				return false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			var pagePostCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.handlerFail(w, r, &pagePostCount) {
					return
				}
				if handleCanvasMockSuccessRequest(t, w, r, &pagePostCount) {
					return
				}
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}))
			defer server.Close()

			cmd, cfgPath := publishTestSetup(t, dir, server.URL)
			err := cmd.Run(context.Background(), &commands.CLI{Config: cfgPath})
			if err == nil || !strings.Contains(err.Error(), tt.errLike) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}
