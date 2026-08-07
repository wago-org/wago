// Package list defines wago version list.
package list

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	List()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "list", Aliases: []string{"ls", "list-installed", "ls-installed", "list-local"},
		Summary:    "list installed runtime versions",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.List() },
	}
}
