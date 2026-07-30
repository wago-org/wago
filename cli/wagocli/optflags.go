//go:build !wago_manager

package wagocli

import (
	"fmt"

	"github.com/wago-org/wago"
)

// Optimization-knob CLI surface. Every codegen knob the active railshot backend
// exposes (wago.OptKnobs) gets a `--<name>` / `--no-<name>` flag pair on the run
// command. Knobs default from WAGO_* env vars; a flag overrides at runtime
// (precedence: flag > env > built-in).

// optKnobFlags builds the generated flag pair (`--<name>`, `--no-<name>`) for
// every knob, so parse() accepts them and help lists them.
func optKnobFlags() []Flag {
	knobs := wago.OptKnobs()
	flags := make([]Flag, 0, len(knobs)*2)
	for _, k := range knobs {
		state := "off"
		if k.On {
			state = "on"
		}
		flags = append(flags,
			Flag{Name: k.Name, Bool: true, Help: fmt.Sprintf("(default: %s) %s", state, k.Desc)},
			Flag{Name: "no-" + k.Name, Bool: true},
		)
	}
	return flags
}

// applyOptFlags applies the knob flags parsed into c before any compilation.
// `--<name>` forces on, `--no-<name>` forces off; giving both fatals.
func applyOptFlags(c *Ctx) {
	for _, k := range wago.OptKnobs() {
		on, off := c.Bool(k.Name), c.Bool("no-"+k.Name)
		if on && off {
			fatal("run: conflicting --%s and --no-%s", k.Name, k.Name)
		}
		switch {
		case on:
			wago.SetOptKnob(k.Name, true)
		case off:
			wago.SetOptKnob(k.Name, false)
		}
	}
}
