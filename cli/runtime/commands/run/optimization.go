package run

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/internal/ui"
)

const deferredBoundsCheckingFlag = "deferred-bounds-checking"

// DeferredBoundsCheckingFlags exposes the runtime-configured bounds-check
// optimization with the same paired boolean surface as backend knobs.
func DeferredBoundsCheckingFlags() []command.Flag {
	state := "on"
	if config, configured, err := settings.LoadConfigured(); err == nil && configured && !config.Runtime.DeferredBoundsChecking {
		state = "off"
	}
	return []command.Flag{
		{Name: deferredBoundsCheckingFlag, Bool: true, Help: fmt.Sprintf("(default: %s) skip provably redundant explicit bounds checks", state)},
		{Name: "no-" + deferredBoundsCheckingFlag, Bool: true},
	}
}

// DeferredBoundsChecking resolves the paired CLI flags. The optimization is on
// by default, matching wago.NewRuntimeConfig.
func DeferredBoundsChecking(ctx *command.Ctx, defaultValue bool) (bool, error) {
	on := ctx.Bool(deferredBoundsCheckingFlag)
	off := ctx.Bool("no-" + deferredBoundsCheckingFlag)
	if on && off {
		return false, fmt.Errorf("conflicting --%s and --no-%s", deferredBoundsCheckingFlag, deferredBoundsCheckingFlag)
	}
	if on {
		return true, nil
	}
	if off {
		return false, nil
	}
	return defaultValue, nil
}

// OptimizationFlags exposes every backend knob as --<name>/--no-<name>.
func OptimizationFlags() []command.Flag {
	knobs := settings.Optimizations()
	configured, hasConfig, _ := settings.LoadConfigured()
	flags := make([]command.Flag, 0, len(knobs)*2)
	for _, knob := range knobs {
		state := "off"
		on := knob.Default
		if hasConfig {
			on = knob.Value(configured)
		}
		if on {
			state = "on"
		}
		flags = append(flags,
			command.Flag{Name: knob.Name(), Bool: true, Help: fmt.Sprintf("(default: %s) %s", state, knob.Description)},
			command.Flag{Name: "no-" + knob.Name(), Bool: true},
		)
	}
	return flags
}

// ApplyOptimizationFlags applies explicit CLI overrides before compilation.
func ApplyOptimizationFlags(ctx *command.Ctx, config *wago.RuntimeConfig) *wago.RuntimeConfig {
	for _, knob := range config.OptimizationInfos() {
		on, off := ctx.Bool(knob.Name), ctx.Bool("no-"+knob.Name)
		if on && off {
			ui.Usage("run: conflicting --%s and --no-%s", knob.Name, knob.Name)
		}
		switch {
		case on:
			config = config.WithOptimization(knob.Name, true)
		case off:
			config = config.WithOptimization(knob.Name, false)
		}
	}
	return config
}

func ApplyOptimizationDefaults(runtimeConfig *wago.RuntimeConfig, config settings.Config, configured bool) *wago.RuntimeConfig {
	if !configured {
		return runtimeConfig
	}
	for name, enabled := range config.Optimizations {
		runtimeConfig = runtimeConfig.WithOptimization(name, enabled)
	}
	return runtimeConfig
}
