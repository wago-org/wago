// Package add defines wago add and wago plugin add.
package add

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Modules        []string
	Global, Local  bool
	Force, Verbose bool
	Capabilities   []string
	GrantAll       bool
	DenyAll        bool
}

type Environment interface {
	Add(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "add", Summary: "add and enable plugins, then rebuild Wago",
		Args: "<module>[@version]...",
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "force", Short: "f", Bool: true, Help: "ignore the build cache / fetch the latest version"},
			{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"},
			{Name: "allow", Arg: "<cap,...>", Help: "grant these capabilities without prompting"},
			{Name: "allow-all", Bool: true, Help: "grant every requested capability without prompting"},
			{Name: "deny-all", Bool: true, Help: "grant no capabilities without prompting"},
		},
		Run: func(c *command.Ctx) {
			explicitChoices := 0
			for _, selected := range []bool{c.Str("allow") != "", c.Bool("allow-all"), c.Bool("deny-all")} {
				if selected {
					explicitChoices++
				}
			}
			if explicitChoices > 1 {
				ui.Fatal("add: choose only one of --allow, --allow-all, or --deny-all")
			}
			options := Options{
				Modules: c.Args, Global: c.Bool("global"), Local: c.Bool("local"),
				Force: c.Bool("force"), Verbose: c.Bool("verbose"),
				Capabilities: plugin.SplitCommaList(c.Str("allow")),
				GrantAll:     c.Bool("allow-all"),
				DenyAll:      c.Bool("deny-all"),
			}
			if len(options.Modules) == 0 {
				ui.Fatal("add: need at least one <module>[@version]")
			}
			environment.Add(options)
		},
	}
}
