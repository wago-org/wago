// Package whoami defines wago auth whoami.
package whoami

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Whoami()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "whoami", Aliases: []string{"who"}, Summary: "print the logged-in account",
		Automation: command.JSONOutput,
		Run:        func(*command.Ctx) { environment.Whoami() },
	}
}
