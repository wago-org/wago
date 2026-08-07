package run

import (
	"github.com/wago-org/wago/cli/internal/command"
	internalparallel "github.com/wago-org/wago/cli/internal/parallel"
)

func ParallelFlag() command.Flag {
	return internalparallel.Flag()
}

func NormalizeParallelArgs(args []string, flags []command.Flag, stopAtPositional bool) ([]string, error) {
	return internalparallel.NormalizeArgs(args, flags, stopAtPositional)
}
