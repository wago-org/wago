package command

import (
	"encoding/json"
	"io"
)

type CommandSchema struct {
	SchemaVersion int           `json:"schemaVersion"`
	Name          string        `json:"name"`
	Flags         []FlagSpec    `json:"flags,omitempty"`
	Commands      []CommandSpec `json:"commands"`
}

type CommandSpec struct {
	Name        string        `json:"name"`
	Aliases     []string      `json:"aliases,omitempty"`
	Summary     string        `json:"summary,omitempty"`
	Arguments   string        `json:"arguments,omitempty"`
	PassThrough bool          `json:"passThrough,omitempty"`
	Flags       []FlagSpec    `json:"flags,omitempty"`
	Commands    []CommandSpec `json:"commands,omitempty"`
}

type FlagSpec struct {
	Name     string `json:"name"`
	Short    string `json:"short,omitempty"`
	Type     string `json:"type"`
	Value    string `json:"value,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Category string `json:"category,omitempty"`
}

func Describe(root *Cmd) CommandSchema {
	return CommandSchema{
		SchemaVersion: 1,
		Name:          root.Name,
		Flags: describeFlags([]Flag{
			{Name: "version", Short: "v", Bool: true, Help: "show version information"},
			{Name: "help", Short: "h", Bool: true, Help: "show help or emit the command schema with --json"},
			{Name: "json", Short: "j", Bool: true, Help: "emit machine-readable JSON when supported"},
			{Name: "no-input", Bool: true, Help: "never prompt; fail when required input is missing"},
			{Name: "dry-run", Bool: true, Help: "show supported mutation plans without changing anything"},
			{Name: "locked", Bool: true, Help: "fail rather than change wago-lock.json or wago.json"},
			{Name: "offline", Bool: true, Help: "use only installed and cached resources"},
		}),
		Commands: describeChildren(root.Children),
	}
}

func WriteSchema(out io.Writer, root *Cmd) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Describe(root))
}

func describeChildren(children []*Cmd) []CommandSpec {
	result := make([]CommandSpec, 0, len(children))
	for _, child := range children {
		spec := CommandSpec{
			Name: child.Name, Aliases: append([]string(nil), child.Aliases...),
			Summary: child.Summary, Arguments: child.Args, PassThrough: child.PassThrough,
			Commands: describeChildren(child.Children),
		}
		spec.Flags = describeCommandFlags(child)
		result = append(result, spec)
	}
	return result
}

func describeCommandFlags(cmd *Cmd) []FlagSpec {
	flags := append([]Flag(nil), cmd.Flags...)
	flags = append(flags, cmd.automationFlags()...)
	flags = append(flags, Flag{Name: "help", Short: "h", Bool: true, Help: "show this help"})
	result := describeFlags(flags)
	for _, flag := range describeFlags(cmd.Knobs) {
		flag.Category = "optimization"
		result = append(result, flag)
	}
	return result
}

func describeFlags(flags []Flag) []FlagSpec {
	result := make([]FlagSpec, 0, len(flags))
	for _, flag := range flags {
		kind := "string"
		if flag.Bool {
			kind = "boolean"
		}
		result = append(result, FlagSpec{
			Name: flag.Name, Short: flag.Short, Type: kind, Value: flag.Arg, Summary: flag.Help,
		})
	}
	return result
}
