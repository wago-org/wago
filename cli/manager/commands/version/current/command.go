// Package current defines wago version current.
package current

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Current()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "current", Aliases: []string{"active"}, Summary: "print the active runtime, profile, and build",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Current() },
	}
}
