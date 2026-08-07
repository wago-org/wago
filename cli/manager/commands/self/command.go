// Package self defines the wago self command tree.
package self

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/self/uninstall"
	"github.com/wago-org/wago/cli/manager/commands/self/update"
)

type Environment interface {
	update.Environment
	uninstall.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "self", Summary: "update or uninstall Wago",
		Children: []*command.Cmd{
			update.Command(environment),
			uninstall.Command(environment),
		},
	}
}
