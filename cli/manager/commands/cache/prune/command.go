package prune

import (
	"fmt"
	"strconv"
	"time"

	"github.com/wago-org/wago/cli/internal/automation"
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

const maxPruneDays = int64((1<<63 - 1) / (24 * time.Hour))

func parseDays(value string) (int, error) {
	days, err := strconv.ParseInt(value, 10, 64)
	if err != nil || days < 0 || days > maxPruneDays {
		return 0, fmt.Errorf("must be an integer from 0 through %d", maxPruneDays)
	}
	return int(days), nil
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:       "prune",
		Summary:    "remove caches unused by installed runtimes",
		Automation: command.DryRun,
		Flags: []command.Flag{
			{Name: "days", Short: "d", Arg: "<n>", Help: "minimum age in days (default 30)"},
			{Name: "yes", Short: "y", Bool: true, Help: "confirm pruning"},
		},
		Run: func(ctx *command.Ctx) {
			days := 30
			if value := ctx.Str("days"); value != "" {
				parsed, err := parseDays(value)
				if err != nil {
					ui.Usage("cache prune: --days %v", err)
				}
				days = parsed
			}
			if !ctx.Bool("yes") && !automation.DryRun() {
				ui.Usage("cache prune: pass --yes to confirm")
			}
			environment.CachePrune(Options{Days: days, Yes: true})
		},
	}
}
