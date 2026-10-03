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
	Invoke string
	Core   int
	// Features replaces automatic defaults when FeaturesSet is true.
	// An explicit Core selection takes precedence.
	Features          wago.CoreFeatures
	FeaturesSet       bool
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
		// A nonzero guest exit keeps its requested status. Exit code zero is
		// success only if deferred teardown added no other error to the tree.
		if exit.Code != 0 || onlyGuestExit(err, exit) {
			return int(exit.Code)
		}
	}
	name := "program"
	if len(args) != 0 {
		name = filepath.Base(args[0])
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
	return 1
}

func onlyGuestExit(err error, exit *wago.ExitError) bool {
	if err == exit {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyGuestExit(child, exit) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyGuestExit(wrapped.Unwrap(), exit)
	}
	return false
}

func executeArtifact(artifact []byte, plugins wago.PluginSet, options Options, args []string) (err error) {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	defer finishRuntime(runtime, &err)
	compiled, err := wago.LoadTrustedArtifact(artifact)
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
		return nil, errors.Join(err, runtime.CloseContext(context.Background()))
	}
	return runtime, nil
}

// finishRuntime waits for plugin Stop and close observers. CLI entry points
// return directly to main (and generated standalones call os.Exit), so merely
// publishing asynchronous Runtime.Close would abandon teardown at process exit.
func finishRuntime(runtime *wago.Runtime, result *error) {
	*result = errors.Join(*result, runtime.CloseContext(context.Background()))
}

func finishModule(module *wago.Module, result *error) {
	*result = errors.Join(*result, module.Close())
}

func finishInstance(instance *wago.Instance, result *error) {
	*result = errors.Join(*result, instance.Close())
}

func executeModule(runtime *wago.Runtime, module *wago.Module, options Options, args []string) (err error) {
	// Preserve teardown failures in the returned command status. The defer order
	// closes the instance before its module and the outer caller closes Runtime.
	defer finishModule(module, &err)
	invoke, err := wasmcall.ResolveExport(module.Compiled(), options.Invoke)
	if err != nil {
		return err
	}
	params, results, err := module.Compiled().Signature(invoke)
	if err != nil {
		return err
	}
	if err := wasmcall.ValidateSignature(params, results); err != nil {
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
	defer finishInstance(instance, &err)
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
		if options.FeaturesSet && config.CoreFeatures() != options.Features {
			config = config.WithCoreFeatures(options.Features)
		}
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
