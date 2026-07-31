// Package switchcmd defines wago version switch.
package switchcmd

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Environment interface {
	Switch(version, profile, build string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "switch", Aliases: []string{"use", "swap"},
		Summary: "select a runtime, installing it when needed",
		Args:    "[version]",
		Flags: []command.Flag{
			{Name: "version", Short: "v", Arg: "<version>", Help: "runtime release or channel to select"},
			{Name: "profile", Short: "p", Arg: "<name>", Help: "standard or minimal"},
			{Name: "build", Short: "b", Arg: "<name>", Help: "normal or tiny"},
		},
		Run: func(c *command.Ctx) {
			version := c.Optional("[version]")
			if value := c.Str("version"); value != "" {
				if version != "" {
					ui.Fatal("version switch: use [version] or --version, not both")
				}
				version = value
			}
			environment.Switch(version, c.Str("profile"), c.Str("build"))
		},
	}
}
