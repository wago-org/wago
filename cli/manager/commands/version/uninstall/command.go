// Package uninstall defines wago version uninstall.
package uninstall

import (
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
		Summary: "select and remove installed runtimes",
		Args:    "[version...]",
		Flags:   []command.Flag{{Name: "all", Short: "a", Bool: true, Help: "remove every installed runtime"}},
		Run: func(c *command.Ctx) {
			if c.Bool("all") {
				if len(c.Args) != 0 {
					ui.Fatal("version uninstall: --all cannot be combined with versions")
				}
				environment.UninstallAllVersions()
				return
			}
			environment.UninstallVersions(c.Args)
		},
	}
}
