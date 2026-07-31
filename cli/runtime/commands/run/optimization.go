package run

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
)

const deferredBoundsCheckingFlag = "deferred-bounds-checking"

// DeferredBoundsCheckingFlags exposes the runtime-configured bounds-check
// optimization with the same paired boolean surface as backend knobs.
func DeferredBoundsCheckingFlags() []command.Flag {
	return []command.Flag{
		{Name: deferredBoundsCheckingFlag, Bool: true, Help: "(default: on) skip provably redundant explicit bounds checks"},
		{Name: "no-" + deferredBoundsCheckingFlag, Bool: true},
	}
}

// DeferredBoundsChecking resolves the paired CLI flags. The optimization is on
// by default, matching wago.NewRuntimeConfig.
func DeferredBoundsChecking(ctx *command.Ctx) (bool, error) {
	on := ctx.Bool(deferredBoundsCheckingFlag)
	off := ctx.Bool("no-" + deferredBoundsCheckingFlag)
	if on && off {
		return false, fmt.Errorf("conflicting --%s and --no-%s", deferredBoundsCheckingFlag, deferredBoundsCheckingFlag)
	}
	return !off, nil
}

// OptimizationFlags exposes every backend knob as --<name>/--no-<name>.
func OptimizationFlags() []command.Flag {
	knobs := wago.OptKnobs()
	flags := make([]command.Flag, 0, len(knobs)*2)
	for _, knob := range knobs {
		state := "off"
		if knob.On {
			state = "on"
		}
		flags = append(flags,
			command.Flag{Name: knob.Name, Bool: true, Help: fmt.Sprintf("(default: %s) %s", state, knob.Desc)},
			command.Flag{Name: "no-" + knob.Name, Bool: true},
		)
	}
	return flags
}

// ApplyOptimizationFlags applies explicit CLI overrides before compilation.
func ApplyOptimizationFlags(ctx *command.Ctx) {
	for _, knob := range wago.OptKnobs() {
		on, off := ctx.Bool(knob.Name), ctx.Bool("no-"+knob.Name)
		if on && off {
			ui.Usage("run: conflicting --%s and --no-%s", knob.Name, knob.Name)
		}
		switch {
		case on:
			wago.SetOptKnob(knob.Name, true)
		case off:
			wago.SetOptKnob(knob.Name, false)
		}
	}
}
