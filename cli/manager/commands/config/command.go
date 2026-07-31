// Package config defines wago config.
package config

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/manager/commands/config/completions"
)

func Command(environment completions.Environment) *command.Cmd {
	return &command.Cmd{
		Name:     "config",
		Summary:  "configure Wago",
		Children: []*command.Cmd{completions.Command(environment)},
	}
}
