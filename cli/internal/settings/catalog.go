// Package settings owns Wago's user-configurable runtime defaults.
package settings

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

type BoolSetting struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Default      bool   `json:"default"`
	Experimental bool   `json:"experimental"`
	Available    bool   `json:"available"`
}

var featureSettings = []BoolSetting{
	{"features.bulk-memory-operations", "Bulk memory", "memory.copy, memory.fill, and segment operations", true, false, true},
	{"features.multi-value", "Multi-value", "multiple block and function results", true, false, true},
	{"features.mutable-global", "Mutable globals", "import and export mutable globals", true, false, true},
	{"features.nontrapping-float-to-int-conversion", "Non-trapping conversions", "saturating float-to-integer conversions", true, false, true},
	{"features.reference-types", "Reference types", "funcref, externref, tables, and reference instructions", true, false, true},
	{"features.sign-extension-ops", "Sign extension", "integer sign-extension instructions", true, false, true},
	{"features.simd", "SIMD", "128-bit vector instructions", true, false, true},
	{"features.extended-constant-expressions", "Extended constants", "arithmetic and imported globals in constant expressions", true, false, true},
}

var previewSettings = []BoolSetting{
	{"preview.tail-call", "Tail calls", "return_call and return_call_indirect", false, true, false},
	{"preview.threads-atomics", "Threads and atomics", "shared memory and atomic instructions", false, true, false},
}

var amd64OptimizationSettings = []BoolSetting{
	{"optimizations.bounds-facts", "Bounds facts", "straight-line bounds-check elision", true, false, true},
	{"optimizations.st-flags", "Flags results", "keep comparison results in the flags register", true, false, true},
	{"optimizations.reg-merge", "Register merge", "keep block results in registers across joins", true, false, true},
	{"optimizations.tee-sink", "Tee sinking", "sink local.tee expressions into local registers", true, false, true},
	{"optimizations.unary-sink", "Unary sinking", "sink unary and conversion expressions in place", true, false, true},
	{"optimizations.branch-fold", "Branch folding", "fold branch pairs into one conditional branch", true, false, true},
	{"optimizations.entry-arg-pins", "Entry argument pins", "keep entry arguments in incoming registers", true, false, true},
	{"optimizations.ext-fp-pins", "Extended float pins", "use the larger floating-point register pool", true, false, true},
	{"optimizations.vex-float-mem", "VEX memory operands", "fold scalar float loads into AVX operations", true, false, true},
	{"optimizations.multi-bounds-cert", "Multiple bounds proofs", "retain independent proofs for interleaved arrays", true, false, true},
	{"optimizations.immutable-table", "Immutable tables", "specialize calls through never-written tables", true, false, true},
	{"optimizations.immutable-table-type", "Immutable table types", "skip redundant immutable-table type checks", true, false, true},
	{"optimizations.inline-callfree", "Call-free inline hints", "prioritize call-free functions for inlining", true, false, true},
	{"optimizations.store-forward", "Store forwarding", "forward straight-line stores into loads", true, false, true},
	{"optimizations.frame-elide", "Frame elision", "omit frames for small single-result functions", true, false, true},
	{"optimizations.v128-const-cache", "Vector constant cache", "reserve vector registers for repeated constants", true, false, true},
	{"optimizations.v128-pins", "Vector pins", "pin hot vector locals in registers", true, false, true},
	{"optimizations.v128-sink", "Vector sinking", "sink vector operations into pinned locals", true, false, true},
	{"optimizations.reg-abi", "Register ABI", "use Wago's internal register calling convention", true, false, true},
	{"optimizations.inline", "Inlining", "inline eligible callees", true, false, true},
	{"optimizations.inline-loop-callees", "Loop-call inlining", "inline callees invoked from inside loops", false, true, true},
	{"optimizations.loop-precheck", "Loop prechecks", "hoist invariant bounds checks before loops", true, false, true},
	{"optimizations.stack-fence", "Stack fence", "emit the stack-overflow guard fence", true, false, true},
	{"optimizations.stack-reg", "Stack register", "keep the guest stack pointer in a register", true, false, true},
}

var arm64OptimizationSettings = []BoolSetting{
	{"optimizations.bounds-facts", "Bounds facts", "straight-line bounds-check elision", true, false, true},
	{"optimizations.st-flags", "Flags results", "keep comparison results in the flags register", true, false, true},
	{"optimizations.reg-merge", "Register merge", "keep block results in registers across joins", true, false, true},
	{"optimizations.tee-sink", "Tee sinking", "sink local.tee expressions into local registers", true, false, true},
	{"optimizations.unary-sink", "Unary sinking", "sink unary and conversion expressions in place", true, false, true},
	{"optimizations.three-op-sink", "Three-operand sinking", "sink binary operations into pinned locals", true, false, true},
	{"optimizations.olddest-rhs-sink", "Old destination reuse", "reuse an old destination register as the right operand", true, false, true},
	{"optimizations.branch-fold", "Branch folding", "fold branch pairs into one conditional branch", true, false, true},
	{"optimizations.store-load-fwd", "Store/load forwarding", "forward stores into loads after assembly", true, false, true},
	{"optimizations.uxtw-add", "Extended adds", "fold zero-extension into ADD UXTW", true, false, true},
	{"optimizations.entry-arg-pins", "Entry argument pins", "keep entry arguments in incoming registers", true, false, true},
	{"optimizations.x8-pin", "X8 scratch pin", "pin a scratch value in call-free functions", true, false, true},
	{"optimizations.deep-fp-pins", "Deep float pins", "pin additional float locals in call-free functions", true, false, true},
	{"optimizations.ext-fp-pins", "Extended float pins", "use the larger floating-point register pool", true, false, true},
	{"optimizations.leaf-scratch-pins", "Leaf scratch pins", "pin scratch values in leaf functions", true, false, true},
	{"optimizations.immutable-table", "Immutable tables", "specialize calls through never-written tables", true, false, true},
	{"optimizations.immutable-table-type", "Immutable table types", "skip redundant immutable-table type checks", true, false, true},
	{"optimizations.inline-callfree", "Call-free inline hints", "prioritize call-free functions for inlining", true, false, true},
	{"optimizations.store-forward", "Store forwarding", "forward straight-line stores into loads", true, false, true},
	{"optimizations.frame-elide-reghomed", "Register-homed frames", "omit frames when locals remain in registers", true, false, true},
	{"optimizations.small-frame", "Small frames", "use compact stack adjustment forms", true, false, true},
	{"optimizations.v128-const-cache", "Vector constant cache", "reserve vector registers for repeated constants", true, false, true},
	{"optimizations.v128-pins", "Vector pins", "pin hot vector locals in registers", true, false, true},
	{"optimizations.v128-sink", "Vector sinking", "sink vector operations into pinned locals", true, false, true},
	{"optimizations.reg-abi", "Register ABI", "use Wago's internal register calling convention", true, false, true},
	{"optimizations.inline", "Inlining", "inline eligible callees", true, false, true},
	{"optimizations.inline-loop-callees", "Loop-call inlining", "inline callees invoked from inside loops", false, true, true},
	{"optimizations.loop-precheck", "Loop prechecks", "hoist invariant bounds checks before loops", true, false, true},
	{"optimizations.loop-region-pins", "Loop-region pins", "pin loop-carried values across loop regions", false, true, true},
	{"optimizations.immutable-poly-fastpath", "Polymorphic table fast path", "specialize polymorphic immutable-table calls", false, true, true},
	{"optimizations.legacy-fp-pins", "Legacy float pins", "use the legacy floating-point pin allocator", false, true, true},
	{"optimizations.legacy-gp-pins", "Legacy integer pins", "use the legacy integer pin allocator", false, true, true},
	{"optimizations.stack-fence", "Stack fence", "emit the stack-overflow guard fence", true, false, true},
	{"optimizations.stack-reg", "Stack register", "keep the guest stack pointer in a register", true, false, true},
}

func Features() []BoolSetting { return cloneSettings(featureSettings) }

func Optimizations() []BoolSetting {
	switch runtime.GOARCH {
	case "amd64":
		return cloneSettings(amd64OptimizationSettings)
	case "arm64":
		return cloneSettings(arm64OptimizationSettings)
	default:
		return nil
	}
}

func Experimental() []BoolSetting {
	items := cloneSettings(previewSettings)
	for _, setting := range Optimizations() {
		if setting.Experimental {
			items = append(items, setting)
		}
	}
	return items
}

func AllBoolean() []BoolSetting {
	items := append(Features(), Optimizations()...)
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

func Lookup(key string) (BoolSetting, bool) {
	key = CanonicalKey(key)
	for _, setting := range allKnownBoolean() {
		if setting.Key == key {
			return setting, true
		}
	}
	return BoolSetting{}, false
}

func CanonicalKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "-")
	if strings.Contains(key, ".") {
		return key
	}
	var match string
	for _, setting := range allKnownBoolean() {
		if strings.TrimPrefix(setting.Key, "features.") == key ||
			strings.TrimPrefix(setting.Key, "optimizations.") == key ||
			strings.TrimPrefix(setting.Key, "preview.") == key {
			if match != "" && match != setting.Key {
				return key
			}
			match = setting.Key
		}
	}
	if match != "" {
		return match
	}
	return key
}

func allKnownBoolean() []BoolSetting {
	items := append(Features(), amd64OptimizationSettings...)
	items = append(items, arm64OptimizationSettings...)
	items = append(items, previewSettings...)
	return items
}

func ParseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disable", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q (want: on or off)", value)
	}
}

func cloneSettings(in []BoolSetting) []BoolSetting {
	return append([]BoolSetting(nil), in...)
}
