// Package standalone runs a Wasm command embedded in a native executable.
package standalone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/wasmcall"
)

// Options is the compile-time runtime configuration baked into an executable.
type Options struct {
	Invoke            string
	Core              int
	DeferBoundsChecks bool
	FunctionWorkers   int
	OptimizationKnobs map[string]bool
}

// Run executes source as a command and returns its process exit code. Plugins
// are handed in as one explicit, reviewed PluginSet.
func Run(source []byte, plugins wago.PluginSet, options Options, args []string) int {
	if err := execute(source, plugins, options, args); err != nil {
		var exit *wago.ExitError
		if errors.As(err, &exit) {
			return int(exit.Code)
		}
		name := "program"
		if len(args) != 0 {
			name = filepath.Base(args[0])
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

func execute(source []byte, plugins wago.PluginSet, options Options, args []string) error {
	config, err := runtimeConfig(options)
	if err != nil {
		return err
	}
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config), wago.WithGuestArguments(args))
	defer runtime.Close()
	if err := runtime.LoadPlugins(context.Background(), plugins); err != nil {
		return err
	}
	module, err := runtime.Compile(source)
	if err != nil {
		return err
	}
	invoke, err := wasmcall.ResolveExport(module.Compiled(), options.Invoke)
	if err != nil {
		return err
	}
	params, results, err := module.Compiled().Signature(invoke)
	if err != nil {
		return err
	}
	values := []uint64(nil)
	if invoke != "_start" {
		callArgs := args
		if len(callArgs) != 0 {
			callArgs = callArgs[1:]
		}
		values, err = wasmcall.ParseArgs(callArgs, params)
		if err != nil {
			return err
		}
	}
	instance, err := runtime.Instantiate(context.Background(), module)
	if err != nil {
		return err
	}
	defer instance.Close()
	result, err := instance.Invoke(invoke, values...)
	if err != nil {
		return err
	}
	if invoke != "_start" {
		fmt.Println(wasmcall.Format(invoke, values, result, params, results))
	}
	return nil
}

func runtimeConfig(options Options) (*wago.RuntimeConfig, error) {
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(options.DeferBoundsChecks).WithFunctionWorkers(options.FunctionWorkers)
	config = config.WithOptimizations(options.OptimizationKnobs)
	switch options.Core {
	case 0:
	case 2:
		config = config.WithCoreFeatures(wago.CoreFeaturesV2)
	case 3:
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	default:
		return nil, fmt.Errorf("unknown WebAssembly core feature set %d", options.Core)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}
