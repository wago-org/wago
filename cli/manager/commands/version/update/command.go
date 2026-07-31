// Package update defines wago version update.
package update

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	UpdateVersion(args []string, nightly, canary bool, profile, build string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "update",
		Summary: "update an installed release channel",
		Args:    "[channel]",
		Flags: []command.Flag{
			{Name: "nightly", Bool: true, Help: "refresh the latest nightly release"},
			{Name: "canary", Bool: true, Help: "refresh the canary built from main"},
			{Name: "profile", Arg: "<name>", Help: "profile to refresh (default active)"},
			{Name: "build", Arg: "<name>", Help: "normal or tiny (default active)"},
		},
		Run: func(c *command.Ctx) {
			environment.UpdateVersion(
				c.Args, c.Bool("nightly"), c.Bool("canary"), c.Str("profile"), c.Str("build"),
			)
		},
	}
}
