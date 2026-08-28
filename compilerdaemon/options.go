package compilerdaemon

import (
	"fmt"

	"github.com/wago-org/wago"
)

// HostEffect is the JSON-safe daemon representation of one trusted host-call
// contract. Duplicate module/name identities reject instead of silently
// overwriting one another.
type HostEffect struct {
	Module   string                  `json:"module"`
	Name     string                  `json:"name"`
	Contract wago.HostEffectContract `json:"contract"`
}

// CompileOptions is the complete code-generating configuration accepted by
// protocol version 1. Core zero is the same as Core 1. MemoryLimitPages zero
// retains Wago's default 65,536-page limit.
type CompileOptions struct {
	Target            wago.CompilerTargetMode    `json:"target"`
	Objective         wago.OptimizationObjective `json:"objective"`
	Fallback          wago.CompilerFallback      `json:"fallback"`
	Bounds            wago.BoundsCheckMode       `json:"bounds"`
	Core              uint8                      `json:"core"`
	FunctionWorkers   int                        `json:"function_workers"`
	MemoryLimitPages  uint32                     `json:"memory_limit_pages,omitempty"`
	DeferBoundsChecks *bool                      `json:"defer_bounds_checks,omitempty"`
	Optimizations     map[string]bool            `json:"optimizations,omitempty"`
	Profile           *wago.CompilerProfile      `json:"profile,omitempty"`
	HostEffects       []HostEffect               `json:"host_effects,omitempty"`
}

func (options CompileOptions) runtimeConfig(cache *wago.FunctionArtifactCache) (*wago.RuntimeConfig, error) {
	if options.Target != wago.TargetCompatibility && options.Target != wago.TargetNative {
		return nil, fmt.Errorf("compiler daemon: unsupported target %s", options.Target)
	}
	if !options.Objective.Valid() {
		return nil, fmt.Errorf("compiler daemon: unsupported objective %s", options.Objective)
	}
	if options.Fallback != wago.CompilerFallbackNone && options.Fallback != wago.CompilerFallbackRailshot {
		return nil, fmt.Errorf("compiler daemon: unsupported fallback %s", options.Fallback)
	}
	if options.Bounds != wago.BoundsChecksExplicit && options.Bounds != wago.BoundsChecksSignalsBased {
		return nil, fmt.Errorf("compiler daemon: unsupported bounds mode %s", options.Bounds)
	}
	if options.Core > 3 {
		return nil, fmt.Errorf("compiler daemon: unsupported core version %d", options.Core)
	}
	if options.FunctionWorkers < 0 {
		return nil, fmt.Errorf("compiler daemon: function workers must be non-negative")
	}
	hostEffects := make(map[wago.HostImport]wago.HostEffectContract, len(options.HostEffects))
	for index, effect := range options.HostEffects {
		key := wago.HostImport{Module: effect.Module, Name: effect.Name}
		if _, exists := hostEffects[key]; exists {
			return nil, fmt.Errorf("compiler daemon: duplicate host effect %q.%q", effect.Module, effect.Name)
		}
		if err := effect.Contract.Validate(); err != nil {
			return nil, fmt.Errorf("compiler daemon: host effect %d: %w", index, err)
		}
		hostEffects[key] = effect.Contract
	}
	config := wago.NewRuntimeConfig().
		WithCompiler(wago.CompilerDragline).
		WithCompilerTarget(options.Target).
		WithCompilerFallback(options.Fallback).
		WithOptimizationObjective(options.Objective).
		WithBoundsChecks(options.Bounds).
		WithFunctionWorkers(options.FunctionWorkers).
		WithFunctionArtifactCache(cache).
		WithOptimizations(options.Optimizations).
		WithCompilerProfile(options.Profile).
		WithCompilerHostEffects(hostEffects)
	if options.DeferBoundsChecks != nil {
		config = config.WithDeferBoundsChecks(*options.DeferBoundsChecks)
	}
	switch options.Core {
	case 0, 1:
		config = config.WithCoreFeatures(wago.CoreFeaturesV1)
	case 2:
		config = config.WithCoreFeatures(wago.CoreFeaturesV2)
	case 3:
		config = config.WithCoreFeatures(wago.CoreFeaturesV3)
	}
	if options.MemoryLimitPages != 0 {
		config = config.WithMemoryLimitPages(options.MemoryLimitPages)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("compiler daemon: configuration: %w", err)
	}
	return config, nil
}
