// Package status defines wago status.
package status

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Status()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "status",
		Aliases: []string{"st"},
		Summary: "show the active runtime, project, plugins, and lockfile",
		Run:     func(*command.Ctx) { environment.Status() },
	}
}
