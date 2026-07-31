package clean

import (
	"github.com/wago-org/wago/cli/internal/command"
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
		Name:    "clean",
		Summary: "remove selected regenerable caches",
		Flags:   cacheoptions.Flags(),
		Run: func(ctx *command.Ctx) {
			environment.CacheClean(Options{Selection: cacheoptions.Selected(ctx)})
		},
	}
}
