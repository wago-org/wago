// Package cache defines wago cache.
package cache

import (
	"github.com/wago-org/wago/cli/internal/command"
	cacheclean "github.com/wago-org/wago/cli/manager/commands/cache/clean"
	"github.com/wago-org/wago/cli/manager/commands/cache/dir"
	"github.com/wago-org/wago/cli/manager/commands/cache/prune"
	"github.com/wago-org/wago/cli/manager/commands/cache/size"
)

type Environment interface {
	dir.Environment
	size.Environment
	cacheclean.Environment
	prune.Environment
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "cache",
		Summary: "inspect and clean regenerable Wago data",
		Children: []*command.Cmd{
			dir.Command(environment),
			size.Command(environment),
			prune.Command(environment),
			cacheclean.Command(environment),
		},
	}
}
