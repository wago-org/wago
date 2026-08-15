//go:build wago_lean || (!linux && !windows)

package run

import "github.com/wago-org/wago/cli/internal/command"

func watchFlags() []command.Flag { return nil }

func runWatch(*command.Ctx) bool { return false }
