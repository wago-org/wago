package settings

import (
	"errors"
	"fmt"

	"github.com/wago-org/wago"
	internalparallel "github.com/wago-org/wago/cli/internal/parallel"
)

// CompilationRequest contains the explicit choices that override configured
// runtime defaults for one compilation.
type CompilationRequest struct {
	Arch                   string
	Backend                string
	Target                 string
	Fallback               string
	Objective              string
	Core                   string
	Parallel               string
	DeferredBoundsChecking *bool
	Optimizations          map[string]bool
}

// CompilationSelection is the immutable, architecture-filtered result of
// settings precedence for one runtime compilation configuration.
type CompilationSelection struct {
	Backend                wago.CompilerEngine
	Target                 wago.CompilerTargetMode
	Fallback               wago.CompilerFallback
	Objective              wago.OptimizationObjective
	Core                   int
	FunctionWorkers        int
	DeferredBoundsChecking bool
	Features               map[string]bool
	Optimizations          map[string]bool
}

// CompilationSettingsError distinguishes an unreadable settings source from
// invalid explicit command choices, which remain usage errors at CLI seams.
type CompilationSettingsError struct{ Err error }

func (err *CompilationSettingsError) Error() string { return "load Wago settings: " + err.Err.Error() }
func (err *CompilationSettingsError) Unwrap() error { return err.Err }

func IsCompilationSettingsError(err error) bool {
	var target *CompilationSettingsError
	return errors.As(err, &target)
}

// ResolveCompilation loads the effective settings snapshot once and resolves
// it with explicit choices. Run, build, and standalone compilation share this
// interface so precedence and target filtering cannot drift between callers.
func ResolveCompilation(request CompilationRequest) (CompilationSelection, error) {
	config, configured, err := LoadConfigured()
	if err != nil {
		return CompilationSelection{}, &CompilationSettingsError{Err: err}
	}
	return ResolveCompilationFrom(config, configured, request)
}

// ResolveCompilationFrom resolves a supplied settings snapshot. It is the test
// surface for precedence; production callers normally use ResolveCompilation.
func ResolveCompilationFrom(config Config, configured bool, request CompilationRequest) (CompilationSelection, error) {
	backend, err := resolveBackend(request.Backend)
	if err != nil {
		return CompilationSelection{}, err
	}
	if backend == wago.CompilerDragline && !config.Experimental["dragline"] {
		return CompilationSelection{}, fmt.Errorf("Dragline is experimental; enable it with `wago config --enable dragline --experimental`")
	}
	target, err := resolveTarget(request.Target)
	if err != nil {
		return CompilationSelection{}, err
	}
	fallback, err := resolveFallback(request.Fallback)
	if err != nil {
		return CompilationSelection{}, err
	}
	if fallback == wago.CompilerFallbackRailshot && backend != wago.CompilerDragline {
		return CompilationSelection{}, fmt.Errorf("--compiler-fallback=railshot requires --backend=dragline")
	}
	objective, err := resolveObjective(request.Objective)
	if err != nil {
		return CompilationSelection{}, err
	}
	core, err := resolveCore(request.Core)
	if err != nil {
		return CompilationSelection{}, err
	}
	parallel := request.Parallel
	if parallel == "" && configured {
		parallel = config.Runtime.Parallel
	}
	workers, err := internalparallel.Policy(parallel)
	if err != nil {
		return CompilationSelection{}, err
	}
	deferred := true
	if configured {
		deferred = config.Runtime.DeferredBoundsChecking
	}
	if request.DeferredBoundsChecking != nil {
		deferred = *request.DeferredBoundsChecking
	}

	supported := make(map[string]BoolSetting)
	for _, setting := range OptimizationsForArch(request.Arch) {
		supported[setting.Name()] = setting
	}
	optimizations := make(map[string]bool, len(supported))
	if configured {
		for name, enabled := range config.Optimizations {
			if _, ok := supported[name]; ok {
				optimizations[name] = enabled
			}
		}
	}
	for name, enabled := range request.Optimizations {
		if _, ok := supported[name]; !ok {
			return CompilationSelection{}, fmt.Errorf("optimization %q is unavailable on %s", name, request.Arch)
		}
		optimizations[name] = enabled
	}
	features := map[string]bool{}
	if configured {
		for name, enabled := range config.Features {
			features[name] = enabled
		}
	}
	return CompilationSelection{
		Backend: backend, Target: target, Fallback: fallback, Objective: objective, Core: core, FunctionWorkers: workers, DeferredBoundsChecking: deferred,
		Features: features, Optimizations: optimizations,
	}, nil
}

// RuntimeConfig materializes the public immutable runtime configuration after
// all precedence and architecture filtering have completed.
func (selection CompilationSelection) RuntimeConfig() *wago.RuntimeConfig {
	config := wago.NewRuntimeConfig().
		WithCompiler(selection.Backend).
		WithCompilerTarget(selection.Target).
		WithCompilerFallback(selection.Fallback).
		WithOptimizationObjective(selection.Objective).
		WithDeferBoundsChecks(selection.DeferredBoundsChecking).
		WithFunctionWorkers(selection.FunctionWorkers).
		WithOptimizations(selection.Optimizations)
	switch selection.Core {
	case 2:
		config = config.WithCoreFeatures(wago.CoreFeaturesV2)
	case 3:
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	}
	for name, enabled := range selection.Features {
		if feature, ok := wago.FeatureInfoByName(name); ok && feature.Available {
			config = config.WithFeature(feature.Feature, enabled)
		}
	}
	return config
}

func resolveTarget(value string) (wago.CompilerTargetMode, error) {
	switch value {
	case "", "compat":
		return wago.TargetCompatibility, nil
	case "native":
		return wago.TargetNative, nil
	default:
		return 0, fmt.Errorf("unknown --target %q (want: compat, native)", value)
	}
}

func resolveFallback(value string) (wago.CompilerFallback, error) {
	switch value {
	case "", "none":
		return wago.CompilerFallbackNone, nil
	case "railshot":
		return wago.CompilerFallbackRailshot, nil
	default:
		return 0, fmt.Errorf("unknown --compiler-fallback %q (want: none, railshot)", value)
	}
}

func resolveObjective(value string) (wago.OptimizationObjective, error) {
	switch value {
	case "", "speed":
		return wago.OptimizeSpeed, nil
	case "balanced":
		return wago.OptimizeBalanced, nil
	case "size":
		return wago.OptimizeSize, nil
	default:
		return 0, fmt.Errorf("unknown --objective %q (want: speed, balanced, size)", value)
	}
}

func resolveBackend(value string) (wago.CompilerEngine, error) {
	switch value {
	case "", "railshot":
		return wago.CompilerRailshot, nil
	case "dragline":
		return wago.CompilerDragline, nil
	default:
		return 0, fmt.Errorf("unknown --backend %q (want: railshot, dragline)", value)
	}
}

func resolveCore(value string) (int, error) {
	switch value {
	case "":
		return 0, nil
	case "2":
		return 2, nil
	case "3":
		return 3, nil
	default:
		return 0, fmt.Errorf("unknown --core %q (want: 2, 3)", value)
	}
}
