package clean

import (
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	cacheoptions "github.com/wago-org/wago/cli/manager/commands/cache/options"
)

type Options struct {
	Selection cacheoptions.Selection
}

type Environment interface {
	CacheClean(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "clean",
		Aliases:    []string{"clear"},
		Summary:    "remove selected regenerable caches",
		Automation: command.DryRun,
		Flags:      cacheoptions.Flags(),
		Run: func(ctx *command.Ctx) {
			selection := cacheoptions.Selected(ctx)
			if automation.NoInput() && !selection.Downloads && !selection.Builds {
				ui.Usage("cache clean: --no-input requires --downloads, --builds, or --all")
			}
			environment.CacheClean(Options{Selection: selection})
		},
	}
}
