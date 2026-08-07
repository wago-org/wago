// Package unpublish defines wago plugin unpublish.
package unpublish

import (
	"github.com/wago-org/wago/cli/internal/automation"
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
		Automation: command.DryRun,
		Args:       "<name>[@version]",
		Flags:      []command.Flag{{Name: "yes", Short: "y", Bool: true, Help: "skip the confirmation prompt"}},
		Run: func(c *command.Ctx) {
			if len(c.Args) != 1 {
				ui.Usage("unpublish: need <module-or-short>[@version]")
			}
			if automation.NoInput() && !automation.DryRun() && !c.Bool("yes") {
				ui.Usage("plugin unpublish: --no-input requires --yes")
			}
			environment.Unpublish(Options{Target: c.Args[0], Yes: c.Bool("yes")})
		},
	}
}
