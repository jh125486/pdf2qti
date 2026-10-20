package main_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/generate"
)

func TestMainEntrypoint_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantErr       bool
		wantSubstring string
	}{
		{name: "help", args: []string{"run", ".", "--help"}, wantSubstring: "Usage"},
		{name: "missing config", args: []string{"run", ".", "-c", "/tmp/does-not-exist.json", "validate"}, wantErr: true, wantSubstring: "load config"},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("go", tt.args...) //nolint:gosec // Test-only invocation with static arguments.
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v\noutput:\n%s", err, tt.wantErr, string(out))
			}
			if !strings.Contains(string(out), tt.wantSubstring) {
				t.Fatalf("expected output to contain %q, got:\n%s", tt.wantSubstring, string(out))
			}
		})
	}
}

// TestAgenticHelp runs the installed-style binary outside the repository with no
// environment, so neither working-directory assets nor credentials can supply help.
func TestAgenticHelp(t *testing.T) {
	t.Parallel()
	binary := filepath.Join(t.TempDir(), "pdf2qti")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	configSchema, err := os.ReadFile("quiz_input.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	slides, err := os.ReadFile("slides.md")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
	}{
		{name: "root", args: []string{"--agentic-help"}},
		{name: "missing config and required arguments", args: []string{"--config", "missing.json", "distill", "--agentic-help"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.CommandContext(t.Context(), binary, tt.args...)
			cmd.Dir = t.TempDir()
			cmd.Env = []string{}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("agentic help: %v\n%s", err, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
			var got struct {
				FormatVersion int `json:"format_version"`
				CLI           struct {
					Name  string `json:"name"`
					Flags []struct {
						Name    string  `json:"name"`
						Default *string `json:"default"`
					} `json:"flags"`
					Commands []struct {
						Name  string            `json:"name"`
						Flags []json.RawMessage `json:"flags"`
					} `json:"commands"`
				} `json:"cli"`
				Schemas struct {
					Config    json.RawMessage `json:"config"`
					Questions json.RawMessage `json:"questions"`
					Slides    struct {
						MediaType string `json:"media_type"`
						Content   string `json:"content"`
					} `json:"slides"`
				} `json:"schemas"`
			}
			decoder := json.NewDecoder(&stdout)
			if err := decoder.Decode(&got); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("expected exactly one JSON document; got %v", err)
			}
			if got.FormatVersion != 1 || got.CLI.Name != "pdf2qti" {
				t.Fatalf("unexpected envelope: %+v", got)
			}
			wantCommands := []string{"approve", "distill", "generate", "import-bank", "module", "page", "pptx", "publish", "slides", "validate"}
			var names []string
			for _, command := range got.CLI.Commands {
				names = append(names, command.Name)
			}
			slices.Sort(names)
			if !slices.Equal(names, wantCommands) {
				t.Fatalf("commands = %v, want %v", names, wantCommands)
			}
			flags := map[string]*string{}
			for _, flag := range got.CLI.Flags {
				flags[flag.Name] = flag.Default
			}
			if flags["config"] == nil || *flags["config"] != "quiz_input.json" {
				t.Errorf("config default missing: %v", flags)
			}
			if _, ok := flags["agentic-help"]; !ok {
				t.Error("agentic-help flag missing")
			}
			for _, schema := range []struct {
				name      string
				got, want []byte
			}{
				{name: "config", got: got.Schemas.Config, want: configSchema},
				{name: "questions", got: got.Schemas.Questions, want: mustQuestionSchema(t)},
			} {
				var actual, expected any
				if err := json.Unmarshal(schema.got, &actual); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(schema.want, &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("%s schema differs from canonical source", schema.name)
				}
			}
			if got.Schemas.Slides.MediaType != "text/markdown" || got.Schemas.Slides.Content != string(slides) {
				t.Error("slide schema differs from slides.md")
			}
		})
	}
}

func mustQuestionSchema(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(generate.QuestionSchema().Definition)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
