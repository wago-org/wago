//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/optimization"

// (registry re-exported to package wago via src/wago/railshot_amd64.go)

// Central registry of the CLI-exposable optimization knobs. Each entry points at
// a package-level bool whose built-in default is set from a WAGO_* env var at
// init (env stays the fallback); the CLI overrides it at runtime via SetOptKnob.
// A knob's public sense is always "on = optimization enabled" — `inverted` flips
// that for the handful of vars stored as a DISABLE flag (noStackFence etc.).

type optKnob struct {
	definition optimization.Definition
	ptr        *bool
	inverted   bool // ptr is a disable flag: stored value == !enabled
}

// optKnobRegistry lists every boolean codegen knob. Keep names kebab-case and
// stable — they are the CLI flag surface (`--<name>` / `--no-<name>`).
var optKnobRegistry = []optKnob{
	bindOptKnob("bounds-facts", &boundsFactsEnabled, false),
	bindOptKnob("st-flags", &stFlagsEnabled, false),
	bindOptKnob("reg-merge", &regMergeEnabled, false),
	bindOptKnob("tee-sink", &teeLocalSinkEnabled, false),
	bindOptKnob("unary-sink", &unaryLocalSinkEnabled, false),
	bindOptKnob("branch-fold", &branchFoldEnabled, false),
	bindOptKnob("entry-arg-pins", &entryArgPinsEnabled, false),
	bindOptKnob("ext-fp-pins", &extendedFPPinsEnabled, false),
	bindOptKnob("vex-float-mem", &vexFloatMemEnabled, false),
	bindOptKnob("multi-bounds-cert", &multiBoundsCertEnabled, false),
	bindOptKnob("immutable-table", &immutableLocalTableEnabled, false),
	bindOptKnob("immutable-table-type", &immutableTableTypeEnabled, false),
	bindOptKnob("inline-callfree", &inlineCallFreeHintsEnabled, false),
	bindOptKnob("store-forward", &linearStoreForwardEnabled, false),
	bindOptKnob("frame-elide", &smallFrameElideEnabled, false),
	bindOptKnob("v128-const-cache", &v128ConstCacheEnabled, false),
	bindOptKnob("v128-pins", &v128LocalPinsEnabled, false),
	bindOptKnob("v128-sink", &v128LocalSinkEnabled, false),
	bindOptKnob("reg-abi", &regABIEnabled, false),
	bindOptKnob("inline", &inlineEnabled, false),
	bindOptKnob("inline-loop-callees", &inlineLoopCallees, false),
	bindOptKnob("loop-precheck", &loopPrecheckEnabled, false),
	bindOptKnob("stack-fence", &noStackFence, true),
	bindOptKnob("stack-reg", &noStackReg, true),
}

func bindOptKnob(name string, ptr *bool, inverted bool) optKnob {
	definition, ok := optimization.Lookup("amd64", name)
	if !ok {
		panic("amd64 optimization binding is not registered: " + name)
	}
	return optKnob{definition: definition, ptr: ptr, inverted: inverted}
}

// KnobInfo describes one optimization knob for the CLI: its stable name, a
// one-line description, and whether it is currently enabled.
type KnobInfo struct {
	Name         string
	Label        string
	Desc         string
	On           bool
	Default      bool
	Experimental bool
}

// OptKnobs returns the current state of every optimization knob (public sense:
// On == optimization enabled), in registry order.
func OptKnobs() []KnobInfo {
	out := make([]KnobInfo, len(optKnobRegistry))
	for i, k := range optKnobRegistry {
		on := *k.ptr
		if k.inverted {
			on = !on
		}
		out[i] = KnobInfo{Name: k.definition.Name, Label: k.definition.Label, Desc: k.definition.Description, On: on, Default: k.definition.Default, Experimental: k.definition.Experimental}
	}
	return out
}

// SetOptKnob forces knob `name` on or off (public sense). Returns false if no
// knob has that name.
func SetOptKnob(name string, on bool) bool {
	for _, k := range optKnobRegistry {
		if k.definition.Name != name {
			continue
		}
		v := on
		if k.inverted {
			v = !v
		}
		*k.ptr = v
		return true
	}
	return false
}
