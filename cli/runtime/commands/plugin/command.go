// Package plugin defines the runtime-owned wago plugin command tree.
package plugin

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/runtime/commands/plugin/inspect"
	"github.com/wago-org/wago/cli/runtime/commands/plugin/list"
)

type Environment interface {
	list.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "plugin", Aliases: []string{"plugins"},
		Summary: "list and inspect compiled plugins",
		Children: []*command.Cmd{
			list.Command(environment),
			inspect.Command(),
		},
	}
}
