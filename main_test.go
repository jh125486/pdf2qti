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
		name    string
		command func(t *testing.T) *exec.Cmd
	}{
		{name: "root", command: func(t *testing.T) *exec.Cmd {
			return exec.CommandContext(t.Context(), binary, "--agentic-help")
		}},
		{name: "missing config and required arguments", command: func(t *testing.T) *exec.Cmd {
			return exec.CommandContext(t.Context(), binary, "--config", "missing.json", "distill", "--agentic-help")
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := tt.command(t)
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

			got := decodeAgenticHelp(t, &stdout)
			assertAgenticCommands(t, &got)
			assertAgenticMetadata(t, &got)
			assertAgenticSchemas(t, &got, configSchema, slides)
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

type agenticHelpValue struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     *string  `json:"default"`
	Enum        []string `json:"enum"`
	Env         []string `json:"env"`
	Short       string   `json:"short"`
}

type agenticHelpCommand struct {
	Name      string               `json:"name"`
	Flags     []agenticHelpValue   `json:"flags"`
	Arguments []agenticHelpValue   `json:"arguments"`
	Commands  []agenticHelpCommand `json:"commands"`
}

type agenticHelpDocument struct {
	FormatVersion int                `json:"format_version"`
	CLI           agenticHelpCommand `json:"cli"`
	Schemas       struct {
		Config    json.RawMessage `json:"config"`
		Questions json.RawMessage `json:"questions"`
		Slides    struct {
			MediaType string `json:"media_type"`
			Content   string `json:"content"`
		} `json:"slides"`
	} `json:"schemas"`
}

func decodeAgenticHelp(t *testing.T, output io.Reader) agenticHelpDocument {
	t.Helper()
	var got agenticHelpDocument
	decoder := json.NewDecoder(output)
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("expected exactly one JSON document; got %v", err)
	}
	return got
}

func assertAgenticCommands(t *testing.T, got *agenticHelpDocument) {
	t.Helper()
	if got.FormatVersion != 1 || got.CLI.Name != "pdf2qti" {
		t.Fatalf("unexpected envelope: %+v", got)
	}
	wantCommands := []string{"approve", "distill", "generate", "import-bank", "module", "page", "pptx", "publish", "slides", "validate"}
	names := make([]string, 0, len(got.CLI.Commands))
	for _, command := range got.CLI.Commands {
		names = append(names, command.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, wantCommands) {
		t.Fatalf("commands = %v, want %v", names, wantCommands)
	}
	flags := map[string]*string{}
	for index := range got.CLI.Flags {
		flag := &got.CLI.Flags[index]
		flags[flag.Name] = flag.Default
	}
	if flags["config"] == nil || *flags["config"] != "quiz_input.json" {
		t.Errorf("config default missing: %v", flags)
	}
	if _, ok := flags["agentic-help"]; !ok {
		t.Error("agentic-help flag missing")
	}
}

func assertAgenticSchemas(t *testing.T, got *agenticHelpDocument, configSchema, slides []byte) {
	t.Helper()
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
}

func assertAgenticMetadata(t *testing.T, got *agenticHelpDocument) {
	t.Helper()
	commands := map[string]agenticHelpCommand{"": got.CLI}
	for _, command := range got.CLI.Commands {
		commands[command.Name] = command
	}
	for _, tt := range []*struct {
		name     string
		command  string
		argument bool
		want     agenticHelpValue
	}{
		{name: "global config default and short flag", want: agenticHelpValue{
			Name: "config", Description: "Path to config file.", Type: "string",
			Default: new("quiz_input.json"), Enum: []string{}, Env: []string{}, Short: "c",
		}},
		{name: "bank behavior enum", command: "import-bank", want: agenticHelpValue{
			Name: "on-existing", Description: "Existing bank behavior.", Type: "string",
			Default: new("fail"), Enum: []string{"fail", "append"}, Env: []string{},
		}},
		{name: "credential environment name", command: "publish", want: agenticHelpValue{
			Name: "canvas-token", Description: "Canvas API token.", Type: "string",
			Enum: []string{}, Env: []string{"CANVAS_TOKEN"},
		}},
		{name: "required template positional", command: "pptx", argument: true, want: agenticHelpValue{
			Name: "template", Description: "Path to PPTX template file.", Type: "string", Required: true,
			Enum: []string{}, Env: []string{},
		}},
		{name: "optional source list positional", command: "distill", argument: true, want: agenticHelpValue{
			Name: "i-ds", Description: "Source IDs to distill.", Type: "[]string",
			Enum: []string{}, Env: []string{},
		}},
		{name: "required path flag", command: "import-bank", want: agenticHelpValue{
			Name: "package", Description: "QTI ZIP package.", Type: "path", Required: true,
			Enum: []string{}, Env: []string{},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values := commands[tt.command].Flags
			if tt.argument {
				values = commands[tt.command].Arguments
			}
			value := findAgenticValue(t, values, tt.want.Name)
			if !reflect.DeepEqual(value, tt.want) {
				t.Fatalf("metadata = %+v, want %+v", value, tt.want)
			}
		})
	}
}

func findAgenticValue(t *testing.T, values []agenticHelpValue, name string) agenticHelpValue {
	t.Helper()
	for index := range values {
		if values[index].Name == name {
			return values[index]
		}
	}
	t.Fatalf("metadata missing for %q", name)
	return agenticHelpValue{}
}
