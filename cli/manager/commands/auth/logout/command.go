// Package logout defines wago auth logout.
package logout

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Logout()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "logout", Summary: "remove stored registry credentials",
		Run: func(*command.Ctx) { environment.Logout() },
	}
}
