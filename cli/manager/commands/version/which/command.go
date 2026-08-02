// Package which defines wago version which.
package which

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Which()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "which", Aliases: []string{"path"}, Summary: "print the active runtime executable path",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Which() },
	}
}
