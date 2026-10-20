// White-box tests exercise metadata serialization for model features that the
// current CLI does not use, and the absent-documentation failure before exit.
package commands

import (
	"reflect"
	"testing"

	"github.com/alecthomas/kong"
)

func TestDescribeCommand(t *testing.T) {
	t.Parallel()
	var cli struct {
		Config string `default:"quiz.json" short:"c" env:"QUIZ_CONFIG" help:"Config file."`
		Render struct {
			Input string `arg:"" help:"Input path."`
			Mode  string `enum:"quick,full" default:"quick" required:"" help:"Render mode."`
		} `cmd:"" help:"Render input."`
	}
	parser, err := kong.New(&cli, kong.Name("fixture"), kong.Description("Fixture commands."))
	if err != nil {
		t.Fatal(err)
	}
	got := describeCommand(parser.Model.Node)
	if got.Name != "fixture" || got.Description != "Fixture commands." {
		t.Fatalf("root = %+v", got)
	}
	if len(got.Commands) != 1 || got.Commands[0].Name != "render" {
		t.Fatalf("commands = %+v", got.Commands)
	}
	flags := make(map[string]valueHelp, len(got.Flags))
	for _, flag := range got.Flags {
		flags[flag.Name] = flag
	}
	checks := []struct {
		name string
		got  valueHelp
		want valueHelp
	}{
		{name: "global flag", got: flags["config"], want: valueHelp{Name: "config", Description: "Config file.", Type: "string", Default: stringPointer("quiz.json"), Enum: []string{}, Env: []string{"QUIZ_CONFIG"}, Short: "c"}},
		{name: "required enum flag", got: got.Commands[0].Flags[0], want: valueHelp{Name: "mode", Description: "Render mode.", Type: "string", Required: true, Default: stringPointer("quick"), Enum: []string{"quick", "full"}, Env: []string{}}},
		{name: "positional", got: got.Commands[0].Arguments[0], want: valueHelp{Name: "input", Description: "Input path.", Type: "string", Required: true, Enum: []string{}, Env: []string{}}},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("metadata = %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestAgenticHelpFlagBeforeReset(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		docs Documentation
	}{
		{name: "missing documents"},
		{name: "missing slides", docs: Documentation{ConfigSchema: []byte(`{}`)}},
		{name: "invalid config schema", docs: Documentation{ConfigSchema: []byte(`invalid`), SlideMarkdown: "# Slides"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := (agenticHelpFlag(false)).BeforeReset(nil, tt.docs); err == nil {
				t.Fatal("expected missing documentation error")
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
