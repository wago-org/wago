// Package update defines wago plugin update.
package update

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Module                 string
	Global, Local, Verbose bool
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
			{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"},
		},
		Run: func(c *command.Ctx) {
			environment.UpdatePlugins(Options{
				Module:  c.Optional("[module]"),
				Global:  c.Bool("global"),
				Local:   c.Bool("local"),
				Verbose: c.Bool("verbose"),
			})
		},
	}
}
