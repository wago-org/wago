// Package unpublish defines wago plugin unpublish.
package unpublish

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Target string
	Yes    bool
}

type Environment interface {
	Unpublish(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name: "unpublish", Summary: "remove a published plugin or one version",
		Args:  "<name>[@version]",
		Flags: []command.Flag{{Name: "yes", Short: "y", Bool: true, Help: "skip the confirmation prompt"}},
		Run: func(c *command.Ctx) {
			if len(c.Args) != 1 {
				ui.Fatal("unpublish: need <module-or-short>[@version]")
			}
			environment.Unpublish(Options{Target: c.Args[0], Yes: c.Bool("yes")})
		},
	}
}
