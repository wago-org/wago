//go:build arm64

package arm64

// CodegenStats is the railshot "explain" dashboard: per-function counters that
// make every later optimization prove itself (docs/no-ir-plan.md P1). Collection
// is opt-in â a *CodegenStats is threaded through the fn only when the caller asks
// (CompileOptions.Stats) or WAGO_EXPLAIN=1 is set. When off, the field is nil and
// every counter method is a no-op (nil-receiver methods), so the hot compile path
// pays nothing.
//
// The counters are the sinks the plan's phases target: MemRefsForcedByStore is
// what P2's alias-aware loads shrink, BoundsChecks is what P6's bounds facts
// elide, Calls[...] by kind is what P5's call work moves between buckets, and the
// Peephole map records which instruction-selection rewrites actually fired.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Explain/debug knobs, parsed once. Kept here next to the stats they drive.
var (
	// explainEnabled prints a per-module CodegenStats dump to stderr after every
	// compile. Independent of the programmatic CompileOptions.Stats sink.
	explainEnabled = os.Getenv("WAGO_EXPLAIN") == "1"
	// debugModGlobals prints the module-pinned-global choices (the #90-era temp
	// print, now first-class).
	debugModGlobals = os.Getenv("WAGO_DEBUG_MODGLOBALS") == "1"
	// pinGlobalK overrides the adaptive module-global pin count K: -1 = auto (the
	// pickModuleGlobals heuristic), 0..len(moduleGlobalRegs) = force that many.
	pinGlobalK = parsePinGlobalK(os.Getenv("WAGO_PIN_GLOBAL_K"))
	// boundsFactsEnabled gates P6.1 straight-line bounds-check elision (explicit
	// mode). WAGO_NO_BOUNDS_FACTS=1 forces every check â the A/B oracle + kill switch.
	boundsFactsEnabled = os.Getenv("WAGO_NO_BOUNDS_FACTS") != "1"
	// stFlagsEnabled gates the stFlags tee-forward window (R1): a compare stored by
	// `local.tee $c` and consumed by the next if/br_if/select fuses into the branch,
	// storing $c with a flag-neutral Cset after the CMP. WAGO_NO_STFLAGS=1 is the
	// A/B oracle + kill switch for this flag-desync-sensitive path.
	stFlagsEnabled = os.Getenv("WAGO_NO_STFLAGS") != "1"

	// fcmpFuseEnabled gates float compare→branch fusion (FCMP + B.cond instead of
	// FCMP + CSET + branch). WAGO_NO_FCMP_FUSE=1 is the A/B oracle.
	fcmpFuseEnabled = os.Getenv("WAGO_NO_FCMP_FUSE") != "1"

	// mulAddFuseEnabled gates MADD/MSUB fusion of add(c, a*b)/sub(c, a*b) into a
	// single multiply-add/-subtract. WAGO_NO_MULADD=1 is the A/B oracle.
	mulAddFuseEnabled = os.Getenv("WAGO_NO_MULADD") != "1"
)

func parsePinGlobalK(s string) int {
	switch s {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	default: // "", "auto", or anything unrecognized
		return -1
	}
}

// CodegenStats holds one function's codegen counters. All fields are zero when a
// phenomenon did not occur; maps are nil until first use.
type CodegenStats struct {
	FuncIdx int    // local function index (0-based over m.Code)
	Name    string // name-section / export name, or "" if anonymous

	// Size.
	CodeBytes     int // emitted machine-code length
	FrameBytes    int // stack frame size (sub sp, N)
	MaxSpillSlots int // high-water operand spill slots

	// Register allocator / condense engine traffic.
	Flushes              int // full operand-stack flushes (control boundaries + calls)
	FlushBelows          int // partial flushes below a fused node
	FlushRoots           int // logical roots examined by full flushes
	FlushDeferredRoots   int // deferred roots forced to condense by full flushes
	FlushBelowRoots      int // logical roots examined by partial flushes
	FlushBelowDeferred   int // deferred roots forced to condense by partial flushes
	CallFlushes          int // full/partial operand flushes emitted for a register-ABI call
	LocalSetDeferred     int // local.set/tee whose source was a deferred Valent block
	Condenses            int // deferred-tree condensations to a register
	Spills               int // registerâslot evictions under pressure
	Reloads              int // slotâregister reloads of a spilled value
	MemRefsForcedByStore int // deferred loads forced out by a store (P2.1 target)

	// Bounds / traps.
	BoundsChecks          int // inline memory-OOB checks emitted (P6 elides these)
	BoundsChecksElidable  int // subset of BoundsChecks a straight-line certificate covers (P6.1 sizing; count-only)
	BoundsChecksInLoop    int // subset emitted inside a loop on a keyable base (P6.2 loop-precheck ceiling; count-only)
	BoundsChecksHoistable int // subset on a loop-INVARIANT local base (not set in the loop) â the P6.2 hoistable target; count-only
	TrapStubs             int // shared cold trap stubs emitted (one per trap code used)

	// Calls, by lowering kind: regabi / mixed / wrapper / host / indirect /
	// crossinstance.
	Calls map[string]int

	// Pins.
	PinnedLocals       int // integer/float locals given a dedicated register
	PinnedGlobalsValue int // hot mutable-int globals value-pinned in this function

	// UnpinnedRetry is set when the pinned compile exhausted the register file
	// (a pathologically deep expression tree) and the function was recompiled with
	// local pinning disabled â a diagnostic flag for such register-heavy functions.
	UnpinnedRetry bool

	// Peephole/instruction-selection rewrites that fired, by stable name.
	Peephole map[string]int
}

// resetFuncStats clears every accumulated counter/map of s, keeping only its
// identity (FuncIdx, Name), so a recompile of the same function (the pinning-off
// retry) starts from a clean slate instead of double-counting the failed attempt.
func resetFuncStats(s *CodegenStats) {
	if s == nil {
		return
	}
	idx, name := s.FuncIdx, s.Name
	*s = CodegenStats{FuncIdx: idx, Name: name}
}

func (s *CodegenStats) setUnpinnedRetry() {
	if s != nil {
		s.UnpinnedRetry = true
	}
}

// --- nil-safe counter methods (no-op when collection is off) ---

func (s *CodegenStats) addFlush() {
	if s != nil {
		s.Flushes++
	}
}
func (s *CodegenStats) addFlushBelow() {
	if s != nil {
		s.FlushBelows++
	}
}
func (s *CodegenStats) addFlushRoot(deferred bool) {
	if s != nil {
		s.FlushRoots++
		if deferred {
			s.FlushDeferredRoots++
		}
	}
}
func (s *CodegenStats) addFlushBelowRoot(deferred bool) {
	if s != nil {
		s.FlushBelowRoots++
		if deferred {
			s.FlushBelowDeferred++
		}
	}
}
func (s *CodegenStats) addCallFlush() {
	if s != nil {
		s.CallFlushes++
	}
}
func (s *CodegenStats) addLocalSetDeferred() {
	if s != nil {
		s.LocalSetDeferred++
	}
}
func (s *CodegenStats) addCondense() {
	if s != nil {
		s.Condenses++
	}
}
func (s *CodegenStats) addSpill() {
	if s != nil {
		s.Spills++
	}
}
func (s *CodegenStats) addReload() {
	if s != nil {
		s.Reloads++
	}
}
func (s *CodegenStats) addForcedLoad() {
	if s != nil {
		s.MemRefsForcedByStore++
	}
}
func (s *CodegenStats) addTrapStub() {
	if s != nil {
		s.TrapStubs++
	}
}
func (s *CodegenStats) addBoundsCheck() {
	if s != nil {
		s.BoundsChecks++
	}
}
func (s *CodegenStats) addBoundsElidable() {
	if s != nil {
		s.BoundsChecksElidable++
	}
}
func (s *CodegenStats) addBoundsInLoop() {
	if s != nil {
		s.BoundsChecksInLoop++
	}
}
func (s *CodegenStats) addBoundsHoistable() {
	if s != nil {
		s.BoundsChecksHoistable++
	}
}
func (s *CodegenStats) addPinnedLocal() {
	if s != nil {
		s.PinnedLocals++
	}
}
func (s *CodegenStats) addPinnedGlobalValue() {
	if s != nil {
		s.PinnedGlobalsValue++
	}
}

// call records one call lowering of the given kind.
func (s *CodegenStats) call(kind string) {
	if s == nil {
		return
	}
	if s.Calls == nil {
		s.Calls = make(map[string]int)
	}
	s.Calls[kind]++
}

// peep records one peephole/instruction-selection rewrite by stable name.
func (s *CodegenStats) peep(name string) {
	if s == nil {
		return
	}
	if s.Peephole == nil {
		s.Peephole = make(map[string]int)
	}
	s.Peephole[name]++
}

// ModuleGlobalPinInfo describes one module-wide globalâregister reservation.
type ModuleGlobalPinInfo struct {
	Global uint32
	Reg    string
}

// ModuleStats aggregates one module's per-function stats plus the module-wide
// decisions. The zero value is ready to collect into.
type ModuleStats struct {
	Funcs            []*CodegenStats
	ModuleGlobalPins []ModuleGlobalPinInfo
	Inline           *InlineReport // inline-candidate detection (nil if not analyzed)
}

// String renders the explain dump: a module summary line, the module-pinned
// globals, then one block per function.
func (ms *ModuleStats) String() string {
	if ms == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== codegen explain: %d function(s) ===\n", len(ms.Funcs))
	if len(ms.ModuleGlobalPins) == 0 {
		fmt.Fprintf(&b, "module-pinned globals: none (K=0)\n")
	} else {
		fmt.Fprintf(&b, "module-pinned globals (K=%d):", len(ms.ModuleGlobalPins))
		for _, p := range ms.ModuleGlobalPins {
			fmt.Fprintf(&b, " g%dâ%s", p.Global, p.Reg)
		}
		b.WriteByte('\n')
	}
	if ms.Inline != nil {
		b.WriteString(ms.Inline.String())
	}
	for _, s := range ms.Funcs {
		if s == nil {
			continue
		}
		b.WriteString(s.report())
	}
	return b.String()
}

// report renders one function's counters as an indented block.
func (s *CodegenStats) report() string {
	if s == nil {
		return ""
	}
	name := s.Name
	if name == "" {
		name = "(anon)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fn#%d %q: code=%dB frame=%dB spill_hi=%d\n",
		s.FuncIdx, name, s.CodeBytes, s.FrameBytes, s.MaxSpillSlots)
	fmt.Fprintf(&b, "    alloc: flushes=%d roots=%d deferred=%d flushBelow=%d roots=%d deferred=%d callFlush=%d localSetDeferred=%d condenses=%d spills=%d reloads=%d forcedLoads=%d\n",
		s.Flushes, s.FlushRoots, s.FlushDeferredRoots, s.FlushBelows, s.FlushBelowRoots, s.FlushBelowDeferred, s.CallFlushes, s.LocalSetDeferred, s.Condenses, s.Spills, s.Reloads, s.MemRefsForcedByStore)
	fmt.Fprintf(&b, "    mem:   bounds=%d elidable=%d inloop=%d hoistable=%d trapStubs=%d   pins: local=%d gval=%d\n",
		s.BoundsChecks, s.BoundsChecksElidable, s.BoundsChecksInLoop, s.BoundsChecksHoistable, s.TrapStubs, s.PinnedLocals, s.PinnedGlobalsValue)
	if len(s.Calls) > 0 {
		fmt.Fprintf(&b, "    calls: %s\n", fmtCountMap(s.Calls))
	}
	if len(s.Peephole) > 0 {
		fmt.Fprintf(&b, "    peep:  %s\n", fmtCountMap(s.Peephole))
	}
	return b.String()
}

// fmtCountMap renders a map[string]int as "k1=v1 k2=v2" in stable key order.
func fmtCountMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

// funcDisplayName resolves a friendly name for local function index localIdx: the
// name-section function name if present, else a matching function export, else "".
// regName maps a Reg to its lowercase AArch64 (arm64) mnemonic for the explain
// dump. Register 31 is the stack pointer in the load/store base position this
// backend uses it in, so it prints as "sp".
func regName(r Reg) string {
	switch r {
	case a64.X0:
		return "x0"
	case a64.X1:
		return "x1"
	case a64.X2:
		return "x2"
	case a64.X3:
		return "x3"
	case a64.X4:
		return "x4"
	case a64.X5:
		return "x5"
	case a64.X6:
		return "x6"
	case a64.X7:
		return "x7"
	case a64.X8:
		return "x8"
	case a64.X9:
		return "x9"
	case a64.X10:
		return "x10"
	case a64.X11:
		return "x11"
	case a64.X12:
		return "x12"
	case a64.X13:
		return "x13"
	case a64.X14:
		return "x14"
	case a64.X15:
		return "x15"
	case a64.X16:
		return "x16"
	case a64.X17:
		return "x17"
	case a64.X18:
		return "x18"
	case a64.X19:
		return "x19"
	case a64.X20:
		return "x20"
	case a64.X21:
		return "x21"
	case a64.X22:
		return "x22"
	case a64.X23:
		return "x23"
	case a64.X24:
		return "x24"
	case a64.X25:
		return "x25"
	case a64.X26:
		return "x26"
	case a64.X27:
		return "x27"
	case a64.X28:
		return "x28"
	case a64.X29: // frame pointer (FP)
		return "x29"
	case a64.X30: // link register (LR)
		return "x30"
	case a64.XZR: // reg 31: stack pointer / zero register
		return "sp"
	default:
		return fmt.Sprintf("x?%d", r)
	}
}

func funcDisplayName(m *wasm.Module, localIdx, importedFuncs int) string {
	global := uint32(importedFuncs + localIdx)
	if n, ok := m.NameSec.FuncName(global); ok && n != "" {
		return n
	}
	for i := range m.Exports {
		ex := &m.Exports[i]
		if ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == global {
			return ex.Name
		}
	}
	return ""
}
