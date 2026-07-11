//go:build amd64

package amd64

// (registry re-exported to package wago via src/wago/railshot_amd64.go)

// Central registry of the CLI-exposable optimization knobs. Each entry points at
// a package-level bool whose built-in default is set from a WAGO_* env var at
// init (env stays the fallback); the CLI overrides it at runtime via SetOptKnob.
// A knob's public sense is always "on = optimization enabled" — `inverted` flips
// that for the handful of vars stored as a DISABLE flag (noStackFence etc.).

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
	{"branch-fold", "fold Jcc/JMP pairs into one inverted conditional branch", &branchFoldEnabled, false},
	{"entry-arg-pins", "pin entry arguments in their incoming registers", &entryArgPinsEnabled, false},
	{"ext-fp-pins", "pin extra float locals in call-free functions", &extendedFPPinsEnabled, false},
	{"immutable-table", "specialize calls through never-written tables", &immutableLocalTableEnabled, false},
	{"immutable-table-type", "skip the type check on immutable-table calls", &immutableTableTypeEnabled, false},
	{"inline-callfree", "hint call-free callees for inlining", &inlineCallFreeHintsEnabled, false},
	{"store-forward", "straight-line store to load forwarding", &linearStoreForwardEnabled, false},
	{"frame-elide", "omit the frame for small single-result functions", &smallFrameElideEnabled, false},
	{"v128-const-cache", "reserve XMM registers for repeated v128 constants", &v128ConstCacheEnabled, false},
	{"v128-pins", "pin hot v128 locals in XMM for call-free functions", &v128LocalPinsEnabled, false},
	{"v128-sink", "sink v128 binary ops into pinned locals (3-operand VEX)", &v128LocalSinkEnabled, false},
	{"reg-abi", "internal register calling convention", &regABIEnabled, false},
	{"inline", "inline eligible callees", &inlineEnabled, false},
	{"inline-loop-callees", "inline callees called from inside a loop", &inlineLoopCallees, false},
	{"loop-precheck", "hoist a loop-invariant bounds check to a pre-loop check", &loopPrecheckEnabled, false},
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
