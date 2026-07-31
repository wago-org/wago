// Package switchcmd defines wago version switch.
package switchcmd

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	Switch(version, profile, build string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "switch", Aliases: []string{"swap"},
		Summary: "select a runtime, installing it when needed",
		Args:    "[version]",
		Flags: []command.Flag{
			{Name: "profile", Arg: "<name>", Help: "standard or minimal"},
			{Name: "build", Arg: "<name>", Help: "normal or tiny"},
		},
		Run: func(c *command.Ctx) {
			environment.Switch(c.Optional("[version]"), c.Str("profile"), c.Str("build"))
		},
	}
}
