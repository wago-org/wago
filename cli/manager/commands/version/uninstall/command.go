// Package uninstall defines wago version uninstall.
package uninstall

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	UninstallVersions(versions []string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "uninstall", Aliases: []string{"remove", "rm"},
		Summary: "select and remove installed runtimes",
		Args:    "[version...]",
		Run: func(c *command.Ctx) {
			environment.UninstallVersions(c.Args)
		},
	}
}
