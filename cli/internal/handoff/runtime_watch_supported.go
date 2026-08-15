//go:build (linux || windows) && !wago_lean

package handoff

import "github.com/wago-org/wago/cli/internal/command"

func runtimeWatchFlags() []command.Flag {
	return []command.Flag{
		{Name: "watch", Short: "w", Bool: true, Help: "rerun when the module changes"},
		{Name: "watch-interval", Arg: "<duration>", Help: "watch polling interval (default 200ms)"},
	}
}
