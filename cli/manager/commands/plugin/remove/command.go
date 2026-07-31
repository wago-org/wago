// Package remove defines wago rm and wago plugin remove.
package remove

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name          string
	Global, Local bool
}

type Environment interface {
	Remove(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "remove", Aliases: []string{"rm"},
		Summary: "remove and disable a plugin", Args: "<name>",
		Automation: command.DryRun,
		Flags:      []command.Flag{plugin.GlobalFlag(), plugin.LocalFlag()},
		Run: func(c *command.Ctx) {
			environment.Remove(Options{
				Name: c.One("<name>"), Global: c.Bool("global"), Local: c.Bool("local"),
			})
		},
	}
}
