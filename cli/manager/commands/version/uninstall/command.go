// Package uninstall defines wago version uninstall.
package uninstall

import (
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Environment interface {
	UninstallVersions(versions []string)
	UninstallAllVersions()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "uninstall", Aliases: []string{"remove", "rm"},
		Summary:    "select and remove installed runtimes",
		Automation: command.DryRun,
		Args:       "[version...]",
		Flags:      []command.Flag{{Name: "all", Short: "a", Bool: true, Help: "remove every installed runtime"}},
		Run: func(c *command.Ctx) {
			if c.Bool("all") {
				if len(c.Args) != 0 {
					ui.Usage("version uninstall: --all cannot be combined with versions")
				}
				environment.UninstallAllVersions()
				return
			}
			if automation.NoInput() && len(c.Args) == 0 {
				ui.Usage("version uninstall: --no-input requires [version...] or --all")
			}
			environment.UninstallVersions(c.Args)
		},
	}
}
