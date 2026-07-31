// Package update defines wago version update.
package update

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Environment interface {
	UpdateVersion(args []string, nightly, canary bool, profile, build, use string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "update",
		Summary: "update an installed release channel",
		Args:    "[channel]",
		Flags: []command.Flag{
			{Name: "channel", Arg: "<name>", Help: "canary or nightly"},
			{Name: "nightly", Bool: true, Help: "refresh the latest nightly release"},
			{Name: "canary", Bool: true, Help: "refresh the canary built from main"},
			{Name: "profile", Arg: "<name>", Help: "profile to refresh (default active)"},
			{Name: "build", Arg: "<name>", Help: "normal or tiny (default active)"},
			{Name: "use", Bool: true, Help: "make the updated runtime active without prompting"},
			{Name: "no-use", Bool: true, Help: "do not make the updated runtime active"},
		},
		Run: func(c *command.Ctx) {
			args := append([]string(nil), c.Args...)
			if value := c.Str("channel"); value != "" {
				if len(args) != 0 {
					ui.Fatal("version update: use [channel] or --channel, not both")
				}
				args = []string{value}
			}
			use := ""
			if c.Bool("use") {
				use = "yes"
			}
			if c.Bool("no-use") {
				if use != "" {
					ui.Fatal("version update: choose --use or --no-use")
				}
				use = "no"
			}
			environment.UpdateVersion(
				args, c.Bool("nightly"), c.Bool("canary"), c.Str("profile"), c.Str("build"), use,
			)
		},
	}
}
