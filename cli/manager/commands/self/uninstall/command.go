// Package uninstall defines wago self uninstall.
package uninstall

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	RequestedMode(value string, yes bool) (string, bool)
	Cancelled()
	UninstallSelf(mode string, yes bool)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "uninstall", Summary: "remove Wago with a selectable cleanup scope",
		Flags: []command.Flag{
			{Name: "mode", Arg: "<full|partial|minimal>", Help: "choose what to remove (interactive by default)"},
			{Name: "yes", Short: "y", Bool: true, Help: "skip confirmation (defaults to full mode)"},
		},
		Run: func(c *command.Ctx) {
			yes := c.Bool("yes")
			mode, ok := environment.RequestedMode(c.Str("mode"), yes)
			if !ok {
				environment.Cancelled()
				return
			}
			environment.UninstallSelf(mode, yes)
		},
	}
}
