package prune

import (
	"strconv"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

type Options struct {
	Days int
	Yes  bool
}

type Environment interface {
	CachePrune(Options)
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "prune",
		Summary: "remove caches unused by installed runtimes",
		Flags: []command.Flag{
			{Name: "days", Arg: "<n>", Help: "minimum age in days (default 30)"},
			{Name: "yes", Short: "y", Bool: true, Help: "confirm pruning"},
		},
		Run: func(ctx *command.Ctx) {
			days := 30
			if value := ctx.Str("days"); value != "" {
				parsed, err := strconv.Atoi(value)
				if err != nil || parsed < 0 {
					ui.Fatal("cache prune: --days must be a non-negative integer")
				}
				days = parsed
			}
			if !ctx.Bool("yes") {
				ui.Fatal("cache prune: pass --yes to confirm")
			}
			environment.CachePrune(Options{Days: days, Yes: true})
		},
	}
}
