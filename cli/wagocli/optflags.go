package wagocli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/wago-org/wago"
)

// Optimization-knob CLI surface. Every codegen knob the active railshot backend
// exposes (wago.OptKnobs) gets a `--<name>` / `--no-<name>` flag pair on the run
// command; `wago opts` lists them. Knobs default from WAGO_* env vars; a flag
// overrides at runtime (precedence: flag > env > built-in). See `wago opts`.

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

// optsCommand is `wago opts`: list every optimization knob, its default state,
// and description. The canonical reference for the run flag pairs.
func optsCommand() *Cmd {
	return &Cmd{
		Name:    "opts",
		Summary: "list compiler optimization knobs (--<knob> / --no-<knob> on run)",
		Long: "Each knob is a codegen optimization toggle. On `wago run`, force it with\n" +
			"--<knob> or --no-<knob>. Defaults come from WAGO_* env vars, overridden by\n" +
			"the flag. The state shown is the current default on this build.",
		Run: func(*Ctx) {
			knobs := wago.OptKnobs()
			sort.Slice(knobs, func(i, j int) bool { return knobs[i].Name < knobs[j].Name })
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "KNOB\tDEFAULT\tDESCRIPTION")
			for _, k := range knobs {
				state := "off"
				if k.On {
					state = "on"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", k.Name, state, k.Desc)
			}
			w.Flush()
		},
	}
}
