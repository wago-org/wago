// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

// Package standalone runs an ahead-of-time compiled command in a native executable.
package standalone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/internal/buildfeatures"
	impl "github.com/wago-org/wago/src/wago"
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

// ImageOptions configures execution of a generated image. Compilation settings
// are applied by the generator.
type ImageOptions struct {
	Invoke            string
	Core              int
	Features          wago.CoreFeatures
	FeaturesSet       bool
	DeferBoundsChecks bool
}

// RunCompiled executes an ahead-of-time compiled module linked into the executable.
func RunCompiled(compiled *wago.Compiled, plugins wago.PluginSet, options Options, args []string) int {
	if err := executeCompiled(compiled, plugins, options, args); err != nil {
		return reportError(err, args)
	}
	return 0
}

// RunCompiledWithoutPlugins executes a precompiled module without plugin bindings.
func RunCompiledWithoutPlugins(compiled *wago.Compiled, options Options, args []string) int {
	config, err := runtimeConfig(options)
	if err != nil {
		return reportError(err, args)
	}
	return runImage(compiled, config, options.Invoke, args)
}

// RunImage executes a generated image without plugin bindings.
func RunImage(compiled *wago.Compiled, options ImageOptions, args []string) int {
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(options.DeferBoundsChecks)
	config, err := selectExecutionFeatures(config, options.Core, options.Features, options.FeaturesSet)
	if err != nil {
		return reportError(err, args)
	}
	return RunConfiguredImage(compiled, config, options.Invoke, args)
}

// RunConfiguredImage executes a linked image with the launcher's resolved
// configuration. Target-specific execution constraints are checked at startup.
func RunConfiguredImage(compiled *wago.Compiled, config *wago.RuntimeConfig, invoke string, args []string) int {
	if err := impl.ValidateStandaloneExecutionConfig(config); err != nil {
		return reportError(err, args)
	}
	return runImage(compiled, config, invoke, args)
}

func runImage(compiled *wago.Compiled, config *wago.RuntimeConfig, invoke string, args []string) int {
	var err error
	if standaloneDirectImage {
		err = executeDirectImage(compiled, config, invoke, args)
	} else {
		runtime := wago.NewRuntime(wago.WithRuntimeConfig(config))
		err = executeCompiledModule(runtime, compiled, invoke, args)
	}
	if err != nil {
		return reportError(err, args)
	}
	return 0
}

func reportError(err error, args []string) int {
	if standaloneGuestExit {
		var exit *wago.ExitError
		if errors.As(err, &exit) {
			// A nonzero guest exit keeps its requested status. Exit code zero is
			// success only if deferred teardown added no other error to the tree.
			if exit.Code != 0 || onlyGuestExit(err, exit) {
				return int(exit.Code)
			}
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

func executeCompiled(compiled *wago.Compiled, plugins wago.PluginSet, options Options, args []string) error {
	runtime, err := loadRuntime(plugins, options, args)
	if err != nil {
		return err
	}
	return executeCompiledModule(runtime, compiled, options.Invoke, args)
}

func executeCompiledModule(runtime *wago.Runtime, compiled *wago.Compiled, invoke string, args []string) (err error) {
	defer finishRuntime(runtime, &err)
	module, err := runtime.AdoptModule(compiled)
	if err != nil {
		_ = compiled.Close()
		return err
	}
	return executeModule(runtime, module, invoke, args)
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

func executeModule(runtime *wago.Runtime, module *wago.Module, export string, args []string) (err error) {
	// Preserve teardown failures in the returned command status. The defer order
	// closes the instance before its module and the outer caller closes Runtime.
	defer finishModule(module, &err)
	invoke, values, results, err := invocationArguments(module.Compiled(), export, args)
	if err != nil {
		return err
	}
	// Resolve invocation arguments before reporting an unbound import.
	if buildfeatures.StandaloneUnboundFunctionImport != "" {
		return unboundFunctionImportError(buildfeatures.StandaloneUnboundFunctionImport)
	}
	instance, err := runtime.Instantiate(context.Background(), module)
	if buildfeatures.StandaloneUnboundStorageDiagnostic != "" || buildfeatures.StandaloneUnboundTagImport != "" || err != nil {
		return err
	}
	return invokeStandaloneInstance(instance, invoke, values, results)
}

func invocationArguments(compiled *wago.Compiled, export string, args []string) (string, []uint64, []wago.ValType, error) {
	var err error
	invoke := buildfeatures.StandaloneInvokeExport
	var params, results []wago.ValType
	if !buildfeatures.StandaloneHasSignature {
		invoke, err = wasmcall.ResolveExport(compiled, export)
		if err != nil {
			return "", nil, nil, err
		}
		params, results, err = compiled.Signature(invoke)
		if err != nil {
			return "", nil, nil, err
		}
		if err := wasmcall.ValidateSignature(params, results); err != nil {
			return "", nil, nil, err
		}
	}
	values := []uint64(nil)
	if invoke != "_start" {
		callArgs := args
		if len(callArgs) != 0 {
			callArgs = callArgs[1:]
		}
		switch {
		case !buildfeatures.StandaloneHasSignature:
			values, err = wasmcall.ParseArgs(callArgs, params)
		case len(buildfeatures.StandaloneParameterTypes) != 0:
			values, err = wasmcall.ParseScalarArgs(callArgs, []byte(buildfeatures.StandaloneParameterTypes))
		case len(callArgs) != 0:
			return "", nil, nil, fmt.Errorf("expected 0 arg(s), got %d", len(callArgs))
		}
		if err != nil {
			return "", nil, nil, err
		}
	}
	return invoke, values, results, nil
}

func invokeStandaloneInstance(instance *wago.Instance, invoke string, values []uint64, results []wago.ValType) (err error) {
	defer finishInstance(instance, &err)
	result, err := instance.Invoke(invoke, values...)
	if err != nil {
		return err
	}
	printStandaloneResults(invoke, result, results)
	return nil
}

func printStandaloneResults(invoke string, result []uint64, results []wago.ValType) {
	if buildfeatures.StandaloneResultTypes != 0 && invoke != "_start" && len(result) != 0 {
		if buildfeatures.StandaloneHasSignature {
			fmt.Println(wasmcall.FormatResults(result, []byte(buildfeatures.StandaloneReturnTypes)))
		} else {
			fmt.Println(wasmcall.FormatResults(result, results))
		}
	}
}

func unboundFunctionImportError(key string) error {
	return fmt.Errorf("module imports %q, but nothing provides it: %w", key, wago.ErrMissingImport)
}

func runtimeConfig(options Options) (*wago.RuntimeConfig, error) {
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(options.DeferBoundsChecks).WithFunctionWorkers(options.FunctionWorkers)
	if len(options.OptimizationKnobs) != 0 {
		config = config.WithOptimizations(options.OptimizationKnobs)
	}
	config, err := selectExecutionFeatures(config, options.Core, options.Features, options.FeaturesSet)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

func selectExecutionFeatures(config *wago.RuntimeConfig, core int, features wago.CoreFeatures, featuresSet bool) (*wago.RuntimeConfig, error) {
	switch core {
	case 0:
		if featuresSet && config.CoreFeatures() != features {
			config = config.WithCoreFeatures(features)
		}
	case 2:
		config = config.WithCoreFeatures(wago.CoreFeaturesV2)
	case 3:
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	default:
		return nil, fmt.Errorf("unknown WebAssembly core feature set %d", core)
	}
	return config, nil
}
