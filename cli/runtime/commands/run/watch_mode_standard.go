//go:build (linux || windows) && !wago_lean

package run

import (
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

func watchFlags() []command.Flag {
	return []command.Flag{
		{Name: "watch", Short: "w", Bool: true, Help: "rerun when the module changes"},
		{Name: "watch-interval", Arg: "<duration>", Help: "watch polling interval (default 200ms)"},
	}
}

func runWatch(ctx *command.Ctx) bool {
	if !ctx.Bool("watch") {
		return false
	}
	if len(ctx.Args) == 0 {
		ui.Usage("run: need a <file>")
	}
	watchModule(ctx.Args[0], ctx.Str("watch-interval"))
	return true
}
