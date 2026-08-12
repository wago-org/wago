// Package publish defines wago plugin publish.
package publish

import "github.com/wago-org/wago/cli/internal/command"

type Options struct {
	Manifest, Notes string
}

type Environment interface {
	Publish(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "publish", Summary: "publish a plugin from wago.json",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "manifest", Short: "m", Arg: "<p>", Help: "manifest path (default wago.json)"},
			{Name: "notes", Short: "n", Arg: "<s>", Help: "release notes"},
		},
		Run: func(c *command.Ctx) {
			environment.Publish(Options{
				Manifest: c.Str("manifest"),
				Notes:    c.Str("notes"),
			})
		},
	}
}
