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

// RunArtifact executes a precompiled module embedded in a native executable.
// Unlike Run, it never invokes Wago's compiler.
func RunArtifact(artifact []byte, plugins wago.PluginSet, options Options, args []string) int {
	if err := executeArtifact(artifact, plugins, options, args); err != nil {
		return reportError(err, args)
	}
	return 0
}

func reportError(err error, args []string) int {
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

func executeArtifact(artifact []byte, plugins wago.PluginSet, options Options, args []string) error {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	defer runtime.Close()
	compiled, err := wago.Load(artifact)
	if err != nil {
		return err
	}
	module, err := runtime.AdoptModule(compiled)
	if err != nil {
		return err
	}
	return executeModule(runtime, module, options, args)
}

func loadRuntime(plugins wago.PluginSet, options Options, args []string) (*wago.Runtime, error) {
	config, err := runtimeConfig(options)
	if err != nil {
		return nil, err
	}
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config), wago.WithGuestArguments(args))
	if err := runtime.LoadPlugins(context.Background(), plugins); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return runtime, nil
}

func executeModule(runtime *wago.Runtime, module *wago.Module, options Options, args []string) error {
	defer module.Close()
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
	if invoke != "_start" && len(result) != 0 {
		fmt.Println(wasmcall.FormatResults(result, results))
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
