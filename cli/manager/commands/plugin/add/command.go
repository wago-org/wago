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
		},
		Run: func(c *command.Ctx) {
			options := Options{
				Modules: c.Args, Global: c.Bool("global"), Local: c.Bool("local"),
				Force: c.Bool("force"), Verbose: c.Bool("verbose"),
			}
			if len(options.Modules) == 0 {
				ui.Fatal("add: need at least one <module>[@version]")
			}
			environment.Add(options)
		},
	}
}
