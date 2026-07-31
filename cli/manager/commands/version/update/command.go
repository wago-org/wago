// Package update defines wago version update.
package update

import (
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Environment interface {
	UpdateVersion(args []string, nightly, canary, force bool, profile, build, use string)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "update",
		Aliases:    []string{"up", "upgrade"},
		Summary:    "update an installed release channel",
		Automation: command.DryRun,
		Args:       "[channel]",
		Flags: []command.Flag{
			{Name: "channel", Short: "c", Arg: "<name>", Help: "canary or nightly"},
			{Name: "nightly", Bool: true, Help: "refresh the latest nightly release"},
			{Name: "canary", Bool: true, Help: "refresh the canary built from main"},
			{Name: "force", Short: "f", Bool: true, Help: "reinstall even when the commit matches"},
			{Name: "profile", Short: "p", Arg: "<name>", Help: "profile to refresh (default active)"},
			{Name: "build", Short: "b", Arg: "<name>", Help: "normal or tiny (default active)"},
			{Name: "use", Short: "u", Bool: true, Help: "make the updated runtime active without prompting"},
			{Name: "no-use", Bool: true, Help: "do not make the updated runtime active"},
		},
		Run: func(c *command.Ctx) {
			args := append([]string(nil), c.Args...)
			if value := c.Str("channel"); value != "" {
				if len(args) != 0 {
					ui.Usage("version update: use [channel] or --channel, not both")
				}
				args = []string{value}
			}
			channels := len(args)
			if c.Bool("nightly") {
				channels++
			}
			if c.Bool("canary") {
				channels++
			}
			if channels > 1 {
				ui.Usage("version update: choose exactly one channel")
			}
			if value := c.Str("profile"); value != "" {
				if _, err := wagopaths.ParseProfile(value); err != nil {
					ui.Usage("version update: %v", err)
				}
			}
			if value := c.Str("build"); value != "" {
				if _, err := wagopaths.ParseBuild(value); err != nil {
					ui.Usage("version update: %v", err)
				}
			}
			use := ""
			if c.Bool("use") {
				use = "yes"
			}
			if c.Bool("no-use") {
				if use != "" {
					ui.Usage("version update: choose --use or --no-use")
				}
				use = "no"
			}
			if automation.NoInput() && use == "" && !automation.DryRun() {
				ui.Usage("version update: --no-input requires --use or --no-use")
			}
			environment.UpdateVersion(
				args, c.Bool("nightly"), c.Bool("canary"), c.Bool("force"), c.Str("profile"), c.Str("build"), use,
			)
		},
	}
}
