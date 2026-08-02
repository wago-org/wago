// Package publish defines wago plugin publish.
package publish

import "github.com/wago-org/wago/cli/internal/command"

type Options struct {
	Manifest, Commit, Notes, Category, Tags string
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
			{Name: "commit", Short: "c", Arg: "<c>", Help: "commit SHA (default: git HEAD)"},
			{Name: "notes", Short: "n", Arg: "<s>", Help: "release notes"},
			{Name: "category", Arg: "<c>", Help: "plugin category"},
			{Name: "tags", Short: "t", Arg: "<a,b>", Help: "comma-separated tags"},
		},
		Run: func(c *command.Ctx) {
			environment.Publish(Options{
				Manifest: c.Str("manifest"),
				Commit:   c.Str("commit"),
				Notes:    c.Str("notes"),
				Category: c.Str("category"),
				Tags:     c.Str("tags"),
			})
		},
	}
}
