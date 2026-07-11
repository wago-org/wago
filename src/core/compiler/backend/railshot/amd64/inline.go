//go:build amd64

package amd64

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Inline-candidate detection (analysis + reporting phase). This computes, for
// every local function, whether it is a good target to splice into its call
// sites, plus a module-level report surfaced via WAGO_EXPLAIN. It performs NO
// codegen change — it is the analysis a later transform phase will consume, and
// it lets us see exactly what (and how much) would inline before touching the
// register allocator.
//
// The conservative candidate class (matching the agreed heuristic) is a small,
// non-recursive LEAF function with a register-ABI int-only signature: the
// classic tiny accessor/helper where the call sequence dominates the actual
// work. Leaf-ness (no calls at all) subsumes non-recursion and rules out
// call_indirect / return_call cycles, so the whole class is trivially safe to
// reason about — an inline of such a body cannot re-enter the inliner.

// inlineMaxBodyBytes is the default encoded-body-size ceiling for a candidate
// (a proxy for how much code each inline site adds). Control-flow callees are
// larger than the tiny straight-line accessors, so this is generous; tune via
// WAGO_INLINE_MAXBYTES.
const inlineMaxBodyBytes = 160

// inlineCallSeqBytes is a rough per-call-site machine-code cost for the call
// sequence an inline removes (arg staging + call + result handling). Used only
// to estimate the saved bytes in the report, so an approximate constant is fine.
const inlineCallSeqBytes = 24

// inlineLoopCallees (WAGO_INLINE_LOOPCALLEE=1) re-enables inlining of leaf callees
// that contain a loop. Off by default: loop-carrying bodies are a net-negative to
// splice (see inlineClass).
var inlineLoopCallees = os.Getenv("WAGO_INLINE_LOOPCALLEE") == "1"

var inlineMaxBytes = func() int {
	if v := os.Getenv("WAGO_INLINE_MAXBYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return inlineMaxBodyBytes
}()

// inlineFacts are the per-function facts the candidacy decision needs.
type inlineFacts struct {
	bodyBytes      int      // encoded body size (proxy for inlined code growth)
	calleeCount    int      // number of direct `call` instructions in the body
	callees        []uint32 // direct call target func indices (global indexing)
	hasControlCall bool     // call_indirect / return_call* / call_ref (inline blocker)
	hasLoop        bool
	hasControlFlow bool // any block/loop/if/else/br*/return/unreachable
	touchesMem     bool // any linear-memory op (load/store/size/grow/bulk)
	touchesGlobal  bool // any global.get/global.set
	params         int
	results        int
	regABIIntOnly  bool // signature fits the int-only register ABI
}

// straightLine reports a body with no control flow at all: it is a single basic
// block ending in the function `end`. Such a callee needs no synthetic control
// frame or edge convergence to inline — the Phase-2 transform class.
func (f inlineFacts) straightLine() bool { return !f.hasControlFlow }

// InlineCandidateInfo is one local function's entry in the report.
type InlineCandidateInfo struct {
	FuncIdx   int    // global function index
	Name      string // display name (from the name section, or func#N)
	BodyBytes int    // encoded body size
	Params    int
	Results   int
	CallSites int    // number of direct call sites targeting this function
	Candidate bool   // meets the conservative inline-candidate class
	Reason    string // why it is (or is not) a candidate
}

// InlineReport is the module-level inline-candidate analysis.
type InlineReport struct {
	Funcs                   []InlineCandidateInfo // one entry per local function
	NumCandidates           int
	TotalInlinableCallSites int // Σ candidate.CallSites
	EstBytesAdded           int // rough: Σ callSites*bodyBytes over candidates
	EstBytesSaved           int // rough: Σ callSites*inlineCallSeqBytes over candidates
	MaxBodyBytes            int // the size ceiling in effect
}

// AnalyzeInlineCandidates runs the inline-candidate detection over a module's
// local functions and returns the report. It is pure analysis (no compilation)
// so it is safe to call independently — e.g. from tooling or tests.
func AnalyzeInlineCandidates(m *wasm.Module) (*InlineReport, error) {
	importedFuncs := m.ImportedFuncCount()
	n := len(m.Code)
	facts := make([]inlineFacts, n)
	for i := range m.Code {
		f, err := scanInlineFacts(m, m.Code[i], i, importedFuncs)
		if err != nil {
			return nil, fmt.Errorf("function %d inline scan: %w", i, err)
		}
		facts[i] = f
	}

	// Call-site counts: for every direct call anywhere in the module, tally the
	// target. Indexed by global function index.
	callSites := make([]int, importedFuncs+n)
	for i := range facts {
		for _, callee := range facts[i].callees {
			if int(callee) < len(callSites) {
				callSites[callee]++
			}
		}
	}

	rep := &InlineReport{MaxBodyBytes: inlineMaxBytes}
	rep.Funcs = make([]InlineCandidateInfo, n)
	for i := range facts {
		globalIdx := importedFuncs + i
		info := InlineCandidateInfo{
			FuncIdx:   globalIdx,
			Name:      funcDisplayName(m, i, importedFuncs),
			BodyBytes: facts[i].bodyBytes,
			Params:    facts[i].params,
			Results:   facts[i].results,
			CallSites: callSites[globalIdx],
		}
		info.Candidate, info.Reason = inlineDecision(facts[i], callSites[globalIdx])
		if info.Candidate {
			rep.NumCandidates++
			rep.TotalInlinableCallSites += info.CallSites
			rep.EstBytesAdded += info.CallSites * facts[i].bodyBytes
			rep.EstBytesSaved += info.CallSites * inlineCallSeqBytes
		}
		rep.Funcs[i] = info
	}
	return rep, nil
}

// inlineClass applies the conservative candidate class to a function's own facts
// (independent of how often it is called) and returns the verdict plus a one-line
// reason. This is what the Phase-2 transform keys off: a call to a class member
// can be spliced wherever it appears.
func inlineClass(f inlineFacts) (bool, string) {
	switch {
	case f.hasControlCall:
		return false, "has call_indirect/return_call"
	case f.calleeCount > 0:
		// Non-leaf callees (ones that themselves call) are excluded: inlining them
		// injects a real call into the caller's straight-line region, whose arg
		// staging interacts with the guard-page pinned-local exclusion and explicit-
		// mode register pressure in ways that regressed sqlite/bignum. Leaf-only keeps
		// the transform to bodies that add no call machinery.
		return false, fmt.Sprintf("non-leaf (%d call(s))", f.calleeCount)
	case !f.regABIIntOnly:
		return false, "signature not int-only reg-ABI"
	case f.hasLoop && !inlineLoopCallees:
		// A leaf callee that contains a LOOP is a net-negative to splice: its loop
		// body lands inside the caller's hot region and adds register pressure /
		// code that outweighs the call it removes. Measured: excluding these speeds
		// Impart's libinjection SQLi rule ~3% and sha256 ~2.7% (both big scan/hash
		// functions), with no measurable regression elsewhere on the corpus (the
		// straight-line and simple-branch leaf helpers — the real inline win, e.g.
		// many_funcs, json serialize — are unaffected). Opt back in for A/B with
		// WAGO_INLINE_LOOPCALLEE=1.
		return false, "leaf callee contains a loop"
	case f.bodyBytes > inlineMaxBytes:
		return false, fmt.Sprintf("too big (%dB > %dB)", f.bodyBytes, inlineMaxBytes)
	default:
		return true, ""
	}
}

// inlineDecision layers the call-site gate on inlineClass for the report (a
// class member with no call sites is unused, not inlinable).
func inlineDecision(f inlineFacts, callSites int) (bool, string) {
	if ok, reason := inlineClass(f); !ok {
		return false, reason
	}
	if callSites == 0 {
		return false, "no call sites"
	}
	loop := ""
	if f.hasLoop {
		loop = ", has loop"
	}
	sl := ""
	if !f.straightLine() {
		sl = ", has control flow"
	}
	return true, fmt.Sprintf("leaf, %dB, %d site(s)%s%s", f.bodyBytes, callSites, loop, sl)
}

// scanInlineFacts collects a single local function's inline facts, from the
// byte-backed body (the DecodeModule path) or the AST body (frontend/test path).
func scanInlineFacts(m *wasm.Module, fn wasm.Func, localIdx, importedFuncs int) (inlineFacts, error) {
	var f inlineFacts
	if ft, ok := m.LocalFuncType(localIdx); ok {
		f.params = len(ft.Params)
		f.results = len(ft.Results)
		f.regABIIntOnly = sigFitsRegABI(ft) && sigIsIntOnly(ft)
	}
	if len(fn.BodyBytes) != 0 {
		if err := scanInlineFactsBytes(fn.BodyBytes, &f); err != nil {
			return f, err
		}
		return f, nil
	}
	scanInlineFactsAST(fn.Body.Instrs, &f)
	return f, nil
}

func scanInlineFactsBytes(body []byte, f *inlineFacts) error {
	f.bodyBytes = len(body)
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		// Control-flow opcodes: unreachable/block/loop/if/else/br/br_if/br_table/
		// return. The single terminating `end` (0x0b) is the body end, not a nested
		// block close, so it is not treated as control flow. (ClassifyInstruction-
		// Immediate handles these structurally, so key off the raw opcode.)
		switch op {
		case 0x00, 0x02, 0x03, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x0f:
			f.hasControlFlow = true
		case 0x23, 0x24: // global.get / global.set
			f.touchesGlobal = true
		}
		if err := wasm.ClassifyInstructionImmediateInto(r, op, &imm); err != nil {
			return err
		}
		if imm.TouchesMemory || imm.UsesBulkMemory {
			f.touchesMem = true
		}
		switch imm.Kind {
		case wasm.InstrCall:
			f.calleeCount++
			f.callees = append(f.callees, imm.Index)
		case wasm.InstrCallIndirect, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect,
			wasm.InstrCallRef, wasm.InstrReturnCallRef:
			f.hasControlCall = true
		case wasm.InstrLoop:
			f.hasLoop = true
		}
	}
	return nil
}

func scanInlineFactsAST(instrs []wasm.Instruction, f *inlineFacts) {
	for i := range instrs {
		in := &instrs[i]
		switch in.Kind {
		case wasm.InstrCall:
			f.calleeCount++
			f.callees = append(f.callees, in.Index)
		case wasm.InstrCallIndirect, wasm.InstrReturnCall, wasm.InstrReturnCallIndirect,
			wasm.InstrCallRef, wasm.InstrReturnCallRef:
			f.hasControlCall = true
		case wasm.InstrLoop:
			f.hasLoop, f.hasControlFlow = true, true
			scanInlineFactsAST(in.Body().Instrs, f)
		case wasm.InstrBlock:
			f.hasControlFlow = true
			scanInlineFactsAST(in.Body().Instrs, f)
		case wasm.InstrIf:
			f.hasControlFlow = true
			scanInlineFactsAST(in.Then(), f)
			scanInlineFactsAST(in.Else(), f)
		case wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable, wasm.InstrReturn, wasm.InstrUnreachable:
			f.hasControlFlow = true
		case wasm.InstrGlobalGet, wasm.InstrGlobalSet:
			f.touchesGlobal = true
		default:
			if instrTouchesMemory(in.Kind) {
				f.touchesMem = true
			}
		}
	}
}

// String renders the inline-candidate report for WAGO_EXPLAIN. Candidates are
// listed first (by descending call-site count), then a compact tally of the
// most common reasons functions were rejected.
func (r *InlineReport) String() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "inline candidates: %d/%d functions, %d inlinable call site(s), ~%dB added / ~%dB call-seq saved (<=%dB bodies)\n",
		r.NumCandidates, len(r.Funcs), r.TotalInlinableCallSites, r.EstBytesAdded, r.EstBytesSaved, r.MaxBodyBytes)

	cands := make([]InlineCandidateInfo, 0, r.NumCandidates)
	rejected := make(map[string]int)
	for _, f := range r.Funcs {
		if f.Candidate {
			cands = append(cands, f)
		} else {
			rejected[reasonBucket(f.Reason)]++
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].CallSites > cands[j].CallSites })
	for _, f := range cands {
		fmt.Fprintf(&b, "  INLINE  %-28s  %d->%d  %s\n", truncName(f.Name), f.Params, f.Results, f.Reason)
	}
	if len(rejected) > 0 {
		buckets := make([]string, 0, len(rejected))
		for k := range rejected {
			buckets = append(buckets, k)
		}
		sort.SliceStable(buckets, func(i, j int) bool { return rejected[buckets[i]] > rejected[buckets[j]] })
		b.WriteString("  rejected:")
		for _, k := range buckets {
			fmt.Fprintf(&b, " %s=%d", k, rejected[k])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// reasonBucket collapses a per-function reason to its category (dropping the
// numeric detail) for the rejected tally.
func reasonBucket(reason string) string {
	switch {
	case strings.HasPrefix(reason, "non-leaf"):
		return "non-leaf"
	case strings.HasPrefix(reason, "too big"):
		return "too-big"
	case strings.HasPrefix(reason, "has call_indirect"):
		return "indirect/return-call"
	case strings.HasPrefix(reason, "signature"):
		return "non-int-sig"
	case strings.HasPrefix(reason, "no call sites"):
		return "unused"
	default:
		return reason
	}
}

func truncName(s string) string {
	const max = 28
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// --- Phase 2: the inline transform (WAGO_INLINE) ---

// inlineEnabled gates the actual splice transform. Detection/reporting above
// runs regardless. It defaults on because Impart-style AS rules spend real time
// in tiny string/range helpers where the call sequence dominates; set
// WAGO_INLINE=0/off/false to disable it for A/B runs.
var inlineEnabled = envDefaultOn(os.Getenv("WAGO_INLINE"))

// envDefaultOn parses a default-on (opt-out) boolean knob: empty/unset means
// enabled; 0/false/off/no disables it.
func envDefaultOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// inlineTarget is a callee that will be spliced at its call sites: a straight-line
// leaf with an int-only register-ABI signature and a small body.
type inlineTarget struct {
	globalIdx   int           // global function index (what a `call` immediate names)
	body        []byte        // the callee's expression bytecode (ends in the terminating `end`)
	params      int           // param count (callee locals 0..params-1)
	nLocals     int           // params + declared locals
	localTypes  []machineType // length nLocals: the callee's local machine types
	resultTypes []machineType // the callee's result machine types
	res0        machineType   // first result type (mtNone if none) — for the single-result merge
	touchesMem  bool          // the body has a linear-memory op (drives the caller's guard-page pin exclusion)
	hasCtrl     bool          // the body has control flow → splice through a synthetic boundary frame
}

// buildInlineTargets returns the straight-line leaf inline candidates keyed by
// GLOBAL function index, or nil when inlining is disabled. Candidacy here is a
// property of the callee alone (the call-site count only mattered for the report),
// so a call to one of these can be spliced wherever it appears.
func buildInlineTargets(m *wasm.Module) map[int]*inlineTarget {
	if !inlineEnabled {
		return nil
	}
	importedFuncs := m.ImportedFuncCount()
	var targets map[int]*inlineTarget
	for i := range m.Code {
		body := m.Code[i].BodyBytes
		if len(body) == 0 {
			continue // AST-only bodies are not spliced (the byte body drives the splice)
		}
		facts, err := scanInlineFacts(m, m.Code[i], i, importedFuncs)
		if err != nil {
			continue
		}
		// The transform class: a leaf (no calls, so non-recursive/acyclic) with an
		// int-only reg-ABI signature and a small body. Memory/global ops and
		// multi-slot (v128/float) declared locals are allowed. Control flow is
		// allowed too: such a callee is spliced through a synthetic block frame that
		// stands in for its function boundary (its `return`/`end` merge there), so
		// the existing block/br/convergence machinery lowers it. A straight-line
		// callee skips the frame entirely (the cheaper fast path).
		if ok, _ := inlineClass(facts); !ok {
			continue
		}
		lt := calleeLocalTypes(m, i)
		if lt == nil {
			continue
		}
		ft, _ := m.LocalFuncType(i)
		var rt []machineType
		res0 := mtNone
		if ft != nil {
			rt = typesOfVals(ft.Results)
			if len(rt) > 0 {
				res0 = rt[0]
			}
		}
		if targets == nil {
			targets = map[int]*inlineTarget{}
		}
		targets[importedFuncs+i] = &inlineTarget{
			globalIdx:   importedFuncs + i,
			body:        body,
			params:      facts.params,
			nLocals:     len(lt),
			localTypes:  lt,
			resultTypes: rt,
			res0:        res0,
			touchesMem:  facts.touchesMem,
			hasCtrl:     facts.hasControlFlow,
		}
	}
	return targets
}

// calleeLocalTypes returns the machine types of a local function's params and
// declared locals, in index order, or nil if the type is unavailable.
func calleeLocalTypes(m *wasm.Module, localIdx int) []machineType {
	ft, ok := m.LocalFuncType(localIdx)
	if !ok {
		return nil
	}
	lt := make([]machineType, 0, len(ft.Params))
	for _, p := range ft.Params {
		lt = append(lt, mtOf(p))
	}
	for _, run := range m.Code[localIdx].Locals.Runs {
		for k := 0; k < int(run.Count); k++ {
			lt = append(lt, mtOf(run.Type))
		}
	}
	return lt
}

// reserveInlineLocals scans the caller body for calls to inline targets and, for
// each distinct spliced callee, reserves its params+locals as fresh frame locals
// PAST f.nLocals (so the prologue's zeroDeclaredLocals — bounded by f.nLocals —
// never touches them; each splice binds/zeroes them itself). Records the base for
// callOp. Must run after assignPinnedLocals (which sizes f.locals): the reserved
// locals are appended unpinned. All splice sites of the same callee share one
// region — inlined bodies never overlap (a straight-line leaf fully completes
// before the next splice), so the region is safely reused.
func (f *fn) reserveInlineLocals(callees []*inlineTarget, targets map[int]*inlineTarget) {
	if len(callees) == 0 {
		return
	}
	f.inlineTargets = targets
	f.inlineBase = make(map[int]int, len(callees))
	for _, t := range callees {
		base := len(f.localType)
		for _, lt := range t.localTypes {
			f.localType = append(f.localType, lt)
			f.localSlot = append(f.localSlot, f.nLocalSlots)
			f.nLocalSlots += lt.stackSlots()
			f.locals = append(f.locals, localDef{reg: regNone, typ: lt, state: lsMem})
		}
		f.inlineBase[t.globalIdx] = base
	}
}

// allCallsWillInline reports whether every call in the caller body is a direct
// call to an inline target — i.e. after splicing, the caller makes no native call
// at all. It mirrors callOp's decision exactly: a direct call is inlined iff its
// target is a non-nil inline target (amd64 inlines a target at every call site,
// with no per-site loop gate), and any indirect / return / call_ref never inlines.
// Requires at least one call (a genuinely call-free function is handled by the
// ordinary hints). Inline targets are call-free leaves (inlineClass), so a true
// result means the spliced body adds no call either.
func allCallsWillInline(caller *wasm.Func, targets map[int]*inlineTarget) bool {
	if targets == nil || len(caller.BodyBytes) == 0 {
		return false
	}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	sawCall := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := wasm.ClassifyInstructionImmediateInto(r, op, &imm); err != nil {
			return false
		}
		switch imm.Kind {
		case wasm.InstrCall:
			sawCall = true
			if targets[int(imm.Index)] == nil {
				return false // a direct call that will not be inlined
			}
		case wasm.InstrReturnCall, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect,
			wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return false // never inlined — the caller keeps a real call
		}
	}
	return sawCall
}

// collectInlinedCallees scans the caller body once and returns the distinct
// inline targets it calls, in first-call order. Computed before frame setup so
// the caller's guard-page pin exclusion can be re-derived from the callees
// (whether any touches memory), and reused by reserveInlineLocals.
func collectInlinedCallees(caller *wasm.Func, targets map[int]*inlineTarget) []*inlineTarget {
	if targets == nil || len(caller.BodyBytes) == 0 {
		return nil
	}
	var out []*inlineTarget
	var seen map[int]bool
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return out
		}
		if err := wasm.ClassifyInstructionImmediateInto(r, op, &imm); err != nil {
			return out
		}
		if imm.Kind != wasm.InstrCall {
			continue
		}
		t := targets[int(imm.Index)]
		if t == nil || seen[t.globalIdx] {
			continue
		}
		if seen == nil {
			seen = map[int]bool{}
		}
		seen[t.globalIdx] = true
		out = append(out, t)
	}
	return out
}

// inlinePlanTouchesMemory reports whether any spliced callee touches linear
// memory — used to extend the caller's touchesMemory for the guard-page pin
// exclusion, since the spliced memory ops run in the caller's frame.
func inlinePlanTouchesMemory(callees []*inlineTarget) bool {
	for _, t := range callees {
		if t.touchesMem {
			return true
		}
	}
	return false
}

// inlineCall splices target t's body at the current call site: it binds the p
// argument operands into the callee's param locals, zeroes the callee's declared
// locals, runs the body with localBase set (remapping the callee's locals onto
// the reserved region), then decouples the results from the reserved slots so a
// later splice of the same callee cannot alias them. A straight-line callee runs
// on the cheap frameless path; a control-flow callee runs under a synthetic
// boundary frame (inlineBodyCtrl).
func (f *fn) inlineCall(t *inlineTarget) error {
	f.stats.call("inline")
	base := f.inlineBase[t.globalIdx]
	f.bindInlineParams(t, base)

	old := f.localBase
	f.localBase = base
	var err error
	if t.hasCtrl {
		err = f.inlineBodyCtrl(t)
	} else {
		err = f.inlineBody(t.body)
	}
	f.localBase = old
	if err != nil {
		return err
	}

	// The callee's results sit on the operand stack; a bare `local.get` result is a
	// lazy stLocalRef into a reserved slot, and a deferred result may read one. A
	// later splice of the same callee rebinds those slots, so realize any operand
	// still referencing the reserved region into a register/value now. (The control-
	// flow path already merged results into canonical slots, so this is a no-op there.)
	f.realizeInlineRange(base, base+t.nLocals)
	return nil
}

// bindInlineParams binds the p argument operands into the callee's param locals
// and zero-initializes its declared locals (wasm zero-init, re-done each splice
// so a call in a loop or a second site always starts from zero). Each declared
// local is cleared across its full slot width (a v128 local clears both halves).
func (f *fn) bindInlineParams(t *inlineTarget, base int) {
	// The p args are the top operands (deepest = param 0). Pop each into its param
	// local. setLocal takes the absolute index (localBase is still 0 here).
	for i := t.params - 1; i >= 0; i-- {
		f.setLocal(base+i, false)
	}
	if t.nLocals > t.params {
		z := f.allocReg(0)
		f.a.XorSelf32(z)
		for i := t.params; i < t.nLocals; i++ {
			for s := 0; s < t.localTypes[i].stackSlots(); s++ {
				f.a.Store64(RSP, f.localOff(base+i)+int32(8*s), z)
			}
			f.locals[base+i].state = lsMem
		}
		f.release(z)
	}
}

// inlineBodyCtrl splices a control-flow callee: it pushes a synthetic block frame
// standing in for the callee's function boundary (0 params — the args were bound
// to locals — with the callee's result types), runs the body through the normal
// opcode driver until that frame's terminating `end` pops it, and routes the
// callee's `return` to that frame. The callee's own blocks/loops/ifs and its
// result merge are lowered by the existing control-flow machinery.
func (f *fn) inlineBodyCtrl(t *inlineTarget) error {
	minCtrl := len(f.ctrl)
	rN := len(t.resultTypes)
	fr := ctrlFrame{
		kind:        cfBlock,
		resultN:     rN,
		branchN:     rN,
		resultTypes: t.resultTypes,
		res0:        t.res0,
		elseSite:    -1,
		height:      f.depth(),
	}
	fr.regMerge1 = f.regMerge && rN == 1 && t.res0 != mtNone && t.res0 != mtV128
	fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()...)
	f.flush()
	f.ctrl = append(f.ctrl, fr)

	prevRet := f.inlineRetFrame
	f.inlineRetFrame = len(f.ctrl) - 1
	err := f.bodyLoop(wasm.NewReader(t.body), minCtrl)
	f.inlineRetFrame = prevRet
	return err
}

// inlineBody runs a straight-line callee body's opcodes on the current operand
// stack (with localBase already set), stopping at the terminating `end`. The
// callee is a leaf with no control flow, so every opcode is a plain (non-control)
// op that emitPlain lowers; results are left on the operand stack.
func (f *fn) inlineBody(body []byte) error {
	r := wasm.NewReader(body)
	for {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x0b: // terminating end: results are on the stack
			return nil
		case 0x01: // nop — the driver's body() handles this outside emitPlain
			continue
		}
		if err := f.emitPlain(r, op); err != nil {
			return err
		}
	}
}

// realizeInlineRange forces any operand-stack entry that references a reserved
// inline local in [lo, hi) into a register/value, so it no longer depends on that
// slot's contents (mirrors realizeLocalRefs, over a range).
func (f *fn) realizeInlineRange(lo, hi int) {
	inRange := func(idx int) bool { return idx >= lo && idx < hi }
	for e := f.s.head.next; e != f.s.head; {
		next := e.next
		switch {
		case e.kind == ekValue && (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && inRange(e.st.idx):
			f.materializeByType(e)
		case e.kind == ekValue && e.st.kind == stMemRef && inRange(e.st.memBorrow()):
			f.materializeByType(e)
		case e.kind == ekDeferred && subtreeRefsLocalRange(e, lo, hi):
			f.condense(e, regNone)
		}
		e = next
	}
}

// subtreeRefsLocalRange reports whether the valent block rooted at e reads any
// local in [lo, hi).
func subtreeRefsLocalRange(e *elem, lo, hi int) bool {
	if e == nil {
		return false
	}
	if e.kind == ekValue {
		return (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.idx >= lo && e.st.idx < hi
	}
	if e.kind == ekDeferred {
		return subtreeRefsLocalRange(e.arg0, lo, hi) || subtreeRefsLocalRange(e.arg1, lo, hi)
	}
	return false
}
