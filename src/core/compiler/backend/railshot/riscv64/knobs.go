//go:build riscv64

package riscv64

// Central registry of the CLI-exposable optimization knobs. Each entry points at
// a package-level bool whose built-in default is set from a WAGO_* env var at
// init (env stays the fallback); the CLI overrides it at runtime via SetOptKnob.
// A knob's public sense is always "on = optimization enabled"; `inverted` flips
// that for the handful of vars stored as a DISABLE flag (noStackFence etc.).
// Names are shared with the amd64 registry wherever the concept matches, so a
// script using `--no-v128-pins` works on either architecture.

type optKnob struct {
	name     string
	desc     string
	ptr      *bool
	inverted bool // ptr is a disable flag: stored value == !enabled
}

// optKnobRegistry lists every boolean codegen knob. Keep names kebab-case and
// stable — they are the CLI flag surface (`--<name>` / `--no-<name>`).
var optKnobRegistry = []optKnob{
	{"bounds-facts", "straight-line bounds-check elision", &boundsFactsEnabled, false},
	{"st-flags", "keep comparison results in the flags register", &stFlagsEnabled, false},
	{"reg-merge", "single-result block values stay in registers across joins", &regMergeEnabled, false},
	{"tee-sink", "sink local.tee expressions into the local's register", &teeLocalSinkEnabled, false},
	{"unary-sink", "sink unary/convert local.set expressions in place", &unaryLocalSinkEnabled, false},
	{"three-op-sink", "sink binary ops into pinned locals (3-operand form)", &threeOperandSinkEnabled, false},
	{"olddest-rhs-sink", "reuse an old-dest register as a binary op's RHS", &oldDestRHSSinkEnabled, false},
	{"branch-fold", "fold branch edges into one native conditional transfer", &branchFoldEnabled, false},
	{"zext-add", "fold i64.add with i64.extend_i32_u", &uxtwAddEnabled, false},
	{"entry-arg-pins", "pin entry arguments in their incoming registers", &entryArgPinsEnabled, false},
	{"x8-pin", "pin a scratch value in x8 for call-free functions", &callFreeX8PinEnabled, false},
	{"deep-fp-pins", "pin extra float locals in call-free functions", &deepFPPinsEnabled, false},
	{"ext-fp-pins", "extend the float pin pool in call-free functions", &extendedFPPinsEnabled, false},
	{"leaf-scratch-pins", "pin scratch values in leaf functions", &leafScratchPinsEnabled, false},
	{"immutable-table", "specialize calls through never-written tables", &immutableLocalTableEnabled, false},
	{"immutable-table-type", "skip the type check on immutable-table calls", &immutableTableTypeEnabled, false},
	{"inline-callfree", "hint call-free callees for inlining", &inlineCallFreeHintsEnabled, false},
	{"store-forward", "straight-line store to load forwarding", &linearStoreForwardEnabled, false},
	{"frame-elide-reghomed", "omit the frame when locals are register-homed", &frameElideRegHomed, false},
	{"small-frame", "use the small-frame stack adjustment form", &smallFrameAdjustEnabled, false},
	{"reg-abi", "internal register calling convention", &regABIEnabled, false},
	{"inline", "inline eligible callees", &inlineEnabled, false},
	{"inline-loop-callees", "inline callees called from inside a loop", &inlineLoopCallees, false},
	{"loop-precheck", "hoist a loop-invariant bounds check to a pre-loop check", &loopPrecheckEnabled, false},
	{"loop-region-pins", "pin loop-carried values across the loop region", &loopRegionPinsEnabled, false},
	{"immutable-poly-fastpath", "polymorphic fast path for immutable-table calls", &immutableLocalPolyFastPath, false},
	{"legacy-fp-pins", "legacy float-pin allocation (fallback)", &legacyFPPinsEnabled, false},
	{"legacy-gp-pins", "legacy integer-pin allocation (fallback)", &legacyGPPinsEnabled, false},
	{"stack-fence", "emit the stack-overflow guard fence", &noStackFence, true},
	{"stack-reg", "keep the guest stack pointer in a register", &noStackReg, true},
}

// KnobInfo describes one optimization knob for the CLI: its stable name, a
// one-line description, and whether it is currently enabled.
type KnobInfo struct {
	Name string
	Desc string
	On   bool
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
		out[i] = KnobInfo{Name: k.name, Desc: k.desc, On: on}
	}
	return out
}

// SetOptKnob forces knob `name` on or off (public sense). Returns false if no
// knob has that name.
func SetOptKnob(name string, on bool) bool {
	for _, k := range optKnobRegistry {
		if k.name != name {
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
