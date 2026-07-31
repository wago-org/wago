// Package grant defines wago plugin grant.
package grant

import (
	"github.com/wago-org/wago/cli/internal/command"
	plugin "github.com/wago-org/wago/cli/manager/commands/plugin"
)

type Options struct {
	Name          string
	Global, Local bool
}

type Environment interface {
	Grant(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "grant", Summary: "review and edit a plugin's granted capabilities", Args: "<name>",
		Flags: []command.Flag{plugin.GlobalFlag(), plugin.LocalFlag()},
		Run: func(c *command.Ctx) {
			environment.Grant(Options{
				Name: c.One("<name>"), Global: c.Bool("global"), Local: c.Bool("local"),
			})
		},
	}
}
