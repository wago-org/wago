//go:build wago_lean || (!linux && !windows)

package handoff

import "github.com/wago-org/wago/cli/internal/command"

func runtimeWatchFlags() []command.Flag { return nil }
