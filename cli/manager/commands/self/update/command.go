// Package update defines wago self update.
package update

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Update()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "update", Summary: "update Wago on its current release channel",
		Run: func(*command.Ctx) { environment.Update() },
	}
}
