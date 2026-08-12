// Package catalog defines wago plugin catalog.
package catalog

import "github.com/wago-org/wago/cli/internal/command"

type Options struct {
	Manifest string
	Check    bool
}

type Environment interface {
	Catalog(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "catalog",
		Summary:    "generate the immutable provider catalog snapshot",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "manifest", Short: "m", Arg: "<p>", Help: "manifest path (default wago.json)"},
			{Name: "check", Bool: true, Help: "verify the committed snapshot is current"},
		},
		Run: func(c *command.Ctx) {
			environment.Catalog(Options{Manifest: c.Str("manifest"), Check: c.Bool("check")})
		},
	}
}
