// Package update defines wago plugin update.
package update

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Module                        string
	Global, Local, Force, Verbose bool
}

type Environment interface {
	UpdatePlugins(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "update", Aliases: []string{"up", "upgrade"},
		Summary: "update plugins to their latest versions, then rebuild", Args: "[module]",
		Flags: []command.Flag{
			plugin.GlobalFlag(), plugin.LocalFlag(),
			{Name: "force", Short: "f", Bool: true, Help: "update and rebuild even when commit hashes match"},
			{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"},
		},
		Run: func(c *command.Ctx) {
			environment.UpdatePlugins(Options{
				Module:  c.Optional("[module]"),
				Global:  c.Bool("global"),
				Local:   c.Bool("local"),
				Force:   c.Bool("force"),
				Verbose: c.Bool("verbose"),
			})
		},
	}
}
