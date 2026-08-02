// Package auth defines the wago auth command tree.
package auth

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/auth/login"
	"github.com/wago-org/wago/cli/manager/commands/auth/logout"
	"github.com/wago-org/wago/cli/manager/commands/auth/whoami"
)

type Environment interface {
	login.Environment
	logout.Environment
	whoami.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "auth",
		Summary: "authenticate to the registry (plugins.wago.sh)",
		Children: []*command.Cmd{
			login.Command(environment),
			logout.Command(environment),
			whoami.Command(environment),
		},
	}
}
