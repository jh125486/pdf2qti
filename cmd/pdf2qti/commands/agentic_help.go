package commands

import (
	"encoding/json"
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/jh125486/pdf2qti/internal/generate"
)

// Documentation holds canonical reference files embedded by the executable.
// ExecuteWithDocumentation requires both files when --agentic-help is requested.
type Documentation struct {
	ConfigSchema  json.RawMessage
	SlideMarkdown string
}

type agenticHelpFlag bool

// BeforeReset runs before Kong resolves environment defaults or validates required
// command arguments, like Kong's built-in --help flag.
func (agenticHelpFlag) BeforeReset(ctx *kong.Context, docs Documentation) error {
	if !json.Valid(docs.ConfigSchema) || docs.SlideMarkdown == "" {
		return fmt.Errorf("agentic help requires embedded config schema and slide documentation; use ExecuteWithDocumentation")
	}
	doc := struct {
		FormatVersion int         `json:"format_version"`
		CLI           commandHelp `json:"cli"`
		Schemas       struct {
			Config    json.RawMessage `json:"config"`
			Questions map[string]any  `json:"questions"`
			Slides    struct {
				MediaType string `json:"media_type"`
				Content   string `json:"content"`
			} `json:"slides"`
		} `json:"schemas"`
	}{FormatVersion: 1, CLI: describeCommand(ctx.Model.Node)}
	doc.Schemas.Config = docs.ConfigSchema
	doc.Schemas.Questions = generate.QuestionSchema().Definition
	doc.Schemas.Slides.MediaType = "text/markdown"
	doc.Schemas.Slides.Content = docs.SlideMarkdown
	encoder := json.NewEncoder(ctx.Kong.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("write agentic help: %w", err)
	}
	ctx.Kong.Exit(0)
	return nil
}

type commandHelp struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Detail      string        `json:"detail,omitempty"`
	Aliases     []string      `json:"aliases,omitempty"`
	Flags       []valueHelp   `json:"flags"`
	Arguments   []valueHelp   `json:"arguments"`
	Commands    []commandHelp `json:"commands"`
}

type valueHelp struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     *string  `json:"default"`
	Enum        []string `json:"enum"`
	Env         []string `json:"env"`
	Aliases     []string `json:"aliases,omitempty"`
	Short       string   `json:"short,omitempty"`
}

func describeCommand(node *kong.Node) commandHelp {
	out := commandHelp{
		Name: node.Name, Description: node.Help, Detail: node.Detail, Aliases: node.Aliases,
		Flags: []valueHelp{}, Arguments: []valueHelp{}, Commands: []commandHelp{},
	}
	for _, flag := range node.Flags {
		value := describeValue(flag.Value)
		value.Env = append(value.Env, flag.Envs...)
		value.Aliases = flag.Aliases
		if flag.Short != 0 {
			value.Short = string(flag.Short)
		}
		out.Flags = append(out.Flags, value)
	}
	if node.Argument != nil {
		out.Arguments = append(out.Arguments, describeValue(node.Argument))
	}
	for _, arg := range node.Positional {
		out.Arguments = append(out.Arguments, describeValue(arg))
	}
	for _, child := range node.Children {
		out.Commands = append(out.Commands, describeCommand(child))
	}
	return out
}

func describeValue(value *kong.Value) valueHelp {
	kind := value.Tag.Type
	if kind == "" {
		kind = value.Target.Type().String()
	}
	if value.IsBool() {
		kind = "bool"
	}
	out := valueHelp{
		Name: value.Name, Description: value.Help, Type: kind, Required: value.Required,
		Enum: []string{}, Env: []string{},
	}
	if value.HasDefault {
		defaultValue := value.Default
		out.Default = &defaultValue
	}
	if value.Enum != "" {
		out.Enum = value.EnumSlice()
	}
	return out
}
