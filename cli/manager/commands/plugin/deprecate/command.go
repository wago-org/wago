// Package deprecate defines wago plugin deprecate.
package deprecate

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Target  string
	Message string
	Undo    bool
}

type Environment interface {
	Deprecate(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "deprecate", Summary: "deprecate a plugin/version", Args: "<name>[@version]",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "message", Short: "m", Arg: "<m>", Help: "deprecation notice"},
			{Name: "undo", Short: "u", Bool: true, Help: "reverse a deprecation"},
		},
		Run: func(c *command.Ctx) {
			if len(c.Args) != 1 {
				ui.Usage("deprecate: need <module-or-short>[@version]")
			}
			environment.Deprecate(Options{
				Target: c.Args[0], Message: c.Str("message"), Undo: c.Bool("undo"),
			})
		},
	}
}
