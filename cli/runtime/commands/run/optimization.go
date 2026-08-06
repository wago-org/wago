package run

import (
	"fmt"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
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
// DeferredBoundsOverride returns only an explicit paired-flag choice. Settings
// precedence belongs to settings.ResolveCompilation.
func DeferredBoundsOverride(ctx *command.Ctx) (*bool, error) {
	on := ctx.Bool(deferredBoundsCheckingFlag)
	off := ctx.Bool("no-" + deferredBoundsCheckingFlag)
	if on && off {
		return nil, fmt.Errorf("conflicting --%s and --no-%s", deferredBoundsCheckingFlag, deferredBoundsCheckingFlag)
	}
	if on {
		value := true
		return &value, nil
	}
	if off {
		value := false
		return &value, nil
	}
	return nil, nil
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

// OptimizationOverrides returns the explicit paired-flag choices without
// applying settings precedence or mutating a runtime configuration.
func OptimizationOverrides(ctx *command.Ctx) (map[string]bool, error) {
	overrides := map[string]bool{}
	for _, knob := range settings.Optimizations() {
		on, off := ctx.Bool(knob.Name()), ctx.Bool("no-"+knob.Name())
		if on && off {
			return nil, fmt.Errorf("conflicting --%s and --no-%s", knob.Name(), knob.Name())
		}
		if on {
			overrides[knob.Name()] = true
		} else if off {
			overrides[knob.Name()] = false
		}
	}
	return overrides, nil
}
