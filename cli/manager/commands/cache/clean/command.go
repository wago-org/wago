package clean

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	cacheoptions "github.com/wago-org/wago/cli/manager/commands/cache/options"
)

type Options struct {
	Selection cacheoptions.Selection
	Yes       bool
}

type Environment interface {
	CacheClean(Options)
}

func Command(environment Environment) *command.Cmd {
	flags := append(cacheoptions.Flags(), command.Flag{Name: "yes", Short: "y", Bool: true, Help: "confirm cache deletion"})
	return &command.Cmd{
		Name:    "clean",
		Summary: "remove selected regenerable caches",
		Flags:   flags,
		Run: func(ctx *command.Ctx) {
			selection := cacheoptions.Selected(ctx)
			if !selection.Downloads && !selection.Builds {
				ui.Fatal("cache clean: choose --downloads, --builds, or --all")
			}
			if !ctx.Bool("yes") {
				ui.Fatal("cache clean: pass --yes to confirm")
			}
			environment.CacheClean(Options{Selection: selection, Yes: true})
		},
	}
}
