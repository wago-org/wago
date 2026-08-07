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
	Core                   string
	Parallel               string
	DeferredBoundsChecking *bool
	Optimizations          map[string]bool
}

// CompilationSelection is the immutable, architecture-filtered result of
// settings precedence for one runtime compilation configuration.
type CompilationSelection struct {
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
		Core: core, FunctionWorkers: workers, DeferredBoundsChecking: deferred,
		Features: features, Optimizations: optimizations,
	}, nil
}

// RuntimeConfig materializes the public immutable runtime configuration after
// all precedence and architecture filtering have completed.
func (selection CompilationSelection) RuntimeConfig() *wago.RuntimeConfig {
	config := wago.NewRuntimeConfig().
		WithDeferBoundsChecks(selection.DeferredBoundsChecking).
		WithFunctionWorkers(selection.FunctionWorkers).
		WithOptimizations(selection.Optimizations)
	if selection.Core == 3 {
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	}
	for name, enabled := range selection.Features {
		if feature, ok := wago.FeatureInfoByName(name); ok && feature.Available {
			config = config.WithFeature(feature.Feature, enabled)
		}
	}
	return config
}

func resolveCore(value string) (int, error) {
	switch value {
	case "", "2":
		return 2, nil
	case "3":
		return 3, nil
	default:
		return 0, fmt.Errorf("unknown --core %q (want: 2, 3)", value)
	}
}
