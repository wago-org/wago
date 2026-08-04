package run

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	internalparallel "github.com/wago-org/wago/cli/internal/parallel"
)

func ParallelFlag() command.Flag {
	return internalparallel.Flag()
}

func NormalizeParallelArgs(args []string, flags []command.Flag, stopAtPositional bool) ([]string, error) {
	return internalparallel.NormalizeArgs(args, flags, stopAtPositional)
}

func Config(core string, deferredBoundsChecking bool, parallel string) (*wago.RuntimeConfig, error) {
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(deferredBoundsChecking)
	switch core {
	case "", "2":
	case "3":
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	default:
		return nil, fmt.Errorf("unknown --core %q (want: 2, 3)", core)
	}
	workers, err := ParallelPolicy(parallel)
	if err != nil {
		return nil, err
	}
	return config.WithFunctionWorkers(workers), nil
}

func ParallelPolicy(parallel string) (int, error) {
	return internalparallel.Policy(parallel)
}
