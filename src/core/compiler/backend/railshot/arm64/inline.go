//go:build arm64

package arm64

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
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
// (a proxy for how much code each inline site adds). Tiny helpers retain the
// measured call-chain win; larger leaves stay as register-ABI calls instead of
// multiplying lowering work and native bytes at every call site. Tune via
// WAGO_INLINE_MAXBYTES for controlled experiments.
const inlineMaxBodyBytes = 16

// inlineCallSeqBytes is a rough per-call-site machine-code cost for the call
// sequence an inline removes (arg staging + call + result handling). Used only
// to estimate the saved bytes in the report, so an approximate constant is fine.
const inlineCallSeqBytes = 24

// Keep the common distinct-target set on the stack. The fixed bound caps
// linear comparisons; callers above it retain exact behavior through a map.
const inlineLinearSeenTargets = 8

// Reuse ordinary caller base maps across functions without allowing one caller
// with a very large inline plan to establish module-lifetime map high-water.
const maxRetainedInlineBases = 64

// inlineDeadBodyEnabled is the rollout/measurement oracle for module-layout
// omission of fully spliced, non-addressable compact callees.
var inlineDeadBodyEnabled = os.Getenv("WAGO_INLINE_DEAD_BODY") != "0"

var inlineMaxBytes = func() int {
	if v := os.Getenv("WAGO_INLINE_MAXBYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return inlineMaxBodyBytes
}()

var compactInlineMaxBytesOverride = func() int {
	if v := os.Getenv("WAGO_COMPACT_INLINE_MAXBYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 255 {
			return n
		}
	}
	return -1
}()

func compactInlineBodyLimit(policy CodegenPolicy) int {
	if compactInlineMaxBytesOverride >= 0 {
		return compactInlineMaxBytesOverride
	}
	return int(policy.MaxCompactInlineBodyBytes)
}

// inlineFacts are the per-function facts the candidacy decision needs.
type inlineFacts struct {
	bodyBytes      int      // encoded body size (proxy for inlined code growth)
	calleeCount    int      // number of direct `call` instructions in the body
	callees        []uint32 // direct call target func indices (global indexing)
	hasControlCall bool     // call_indirect / return_call* / call_ref (inline blocker)
	hasLoop        bool
	hasControlFlow bool // any block/loop/if/else/br*/return/unreachable
	moduleEH       bool // requires caller EH frame planning, so cannot yet inline
	touchesMem     bool // any linear-memory op (load/store/size/grow/bulk)
	touchesGlobal  bool // any global.get/global.set
	params         int
	results        int
	declaredLocals int
	callSites      int
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
	return analyzeInlineCandidates(m, currentCodegenPolicy())
}

func analyzeInlineCandidates(m *wasm.Module, policy CodegenPolicy) (*InlineReport, error) {
	importedFuncs := m.ImportedFuncCount()
	n := len(m.Code)
	facts := make([]inlineFacts, n)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		f, err := scanInlineFacts(m, m.Code[i], i, importedFuncs, classifier)
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

	maxBodyBytes := inlineMaxBytes
	if policy.CompactNative {
		maxBodyBytes = compactInlineBodyLimit(policy)
	}
	rep := &InlineReport{MaxBodyBytes: maxBodyBytes}
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
		facts[i].callSites = callSites[globalIdx]
		if recursiveInlineFactsCandidate(m, i, facts[i], policy) {
			info.Candidate, info.Reason = true, fmt.Sprintf("one-level self-recursive, %dB, %d site(s)", facts[i].bodyBytes, callSites[globalIdx])
		} else {
			info.Candidate, info.Reason = inlineDecision(facts[i], callSites[globalIdx], policy)
		}
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
func inlineClass(f inlineFacts, policy CodegenPolicy) (bool, string) {
	switch {
	case f.moduleEH:
		return false, "requires exception-handling frame"
	case policy.CompactNative && !compactInlineOK(f, policy):
		return false, "native compaction requires proved native-byte win"
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
	case f.hasLoop:
		// A leaf callee that contains a LOOP is excluded: splicing its loop into the
		// caller's hot region adds sustained register pressure and code size, while
		// straight-line and simple-branch leaf helpers retain the ordinary inline win.
		return false, "leaf callee contains a loop"
	case f.bodyBytes > inlineMaxBytes:
		return false, fmt.Sprintf("too big (%dB > %dB)", f.bodyBytes, inlineMaxBytes)
	default:
		return true, ""
	}
}

func compactInlineOK(f inlineFacts, policy CodegenPolicy) bool {
	return !f.moduleEH && !f.hasControlCall && f.calleeCount == 0 &&
		f.callSites == 1 && f.straightLine() && f.bodyBytes <= compactInlineBodyLimit(policy) &&
		f.params <= 1 && f.results <= 1 && f.declaredLocals == 0 &&
		!f.touchesMem && !f.touchesGlobal
}

// inlineDecision layers the call-site gate on inlineClass for the report (a
// class member with no call sites is unused, not inlinable).
func inlineDecision(f inlineFacts, callSites int, policy CodegenPolicy) (bool, string) {
	if ok, reason := inlineClass(f, policy); !ok {
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
func scanInlineFacts(m *wasm.Module, fn wasm.Func, localIdx, importedFuncs int, classifier wasm.ModuleInstructionClassifier) (inlineFacts, error) {
	var f inlineFacts
	if ft, ok := m.LocalFuncType(localIdx); ok {
		f.params = len(ft.Params)
		f.results = len(ft.Results)
		f.regABIIntOnly = sigFitsRegABI(ft) && sigIsIntOnly(ft)
	}
	for _, run := range fn.Locals.Runs {
		f.declaredLocals += int(run.Count)
	}
	if len(fn.BodyBytes) != 0 {
		if err := scanInlineFactsBytesWithClassifier(fn.BodyBytes, &f, classifier); err != nil {
			return f, err
		}
		return f, nil
	}
	scanInlineFactsAST(fn.Body.Instrs, &f)
	return f, nil
}

func scanInlineFactsBytes(body []byte, f *inlineFacts) error {
	return scanInlineFactsBytesWithClassifier(body, f, wasm.ModuleInstructionClassifier{})
}

func scanInlineFactsBytesWithClassifier(body []byte, f *inlineFacts, classifier wasm.ModuleInstructionClassifier) error {
	f.bodyBytes = len(body)
	r := wasm.NewReader(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		if shared.InstructionNeedsInlineBoundary(op, wasm.InstrInvalid) {
			f.hasControlFlow = true
		}
		if shared.InstructionNeedsEHFrame(op, wasm.InstrInvalid) {
			f.moduleEH = true
		}
		switch op {
		case 0x23, 0x24: // global.get / global.set
			f.touchesGlobal = true
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return err
		}
		if shared.InstructionNeedsInlineBoundary(op, imm.Kind) {
			f.hasControlFlow = true
		}
		if shared.InstructionNeedsEHFrame(op, imm.Kind) {
			f.moduleEH = true
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
		if shared.InstructionNeedsInlineBoundary(0, in.Kind) {
			f.hasControlFlow = true
		}
		if shared.InstructionNeedsEHFrame(0, in.Kind) {
			f.moduleEH = true
		}
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

// recursiveInlineEnabled admits one deliberately narrow non-leaf class: a
// small, numeric, single-function module whose direct calls can only target
// itself. Each call site is expanded one level; calls encountered while lowering
// that expansion remain native calls, which bounds code growth and compile work.
var recursiveInlineEnabled = envDefaultOn(os.Getenv("WAGO_RECURSIVE_INLINE"))

// inlineTarget is a callee that will be spliced at its call sites: a straight-line
// leaf with an int-only register-ABI signature and a small body.
type inlineTarget struct {
	body           []byte // the callee's expression bytecode (ends in the terminating `end`)
	globalIdx      int    // global function index (what a `call` immediate names)
	localDeclBytes uint32
	typeStart      uint32
	localTypeEnd   uint32
	resultTypeEnd  uint32
	params         uint32 // param count (callee locals 0..params-1)
	res0           machineType
	touchesMem     bool // the body has a linear-memory op (drives the caller's guard-page pin exclusion)
	touchesGlob    bool // the body reads or writes a global
	hasCtrl        bool // the body has control flow → splice through a synthetic boundary frame
	omitStandalone bool // module layout may omit this unreachable standalone body
}

const inlineRecursiveFlag = uint32(1 << 31)

// inlineTargetData keeps the dense local-function lookup separate from the
// pointer-rich records for admitted callees. Most modules have many functions
// but few inline targets, so one 32-bit slot per function is materially smaller
// than one target (and three slice headers) per function.
type inlineTargetData struct {
	slots   []uint32 // local function index -> targets index + 1; zero is not admitted
	targets []inlineTarget
	types   []machineType
}

// inlineTargetInvalid marks a candidate pruned after construction. Keeping its
// compact slot lets transitive omission analysis inspect the original candidate
// without making it reachable to ordinary call-site lookup.
const inlineTargetInvalid = uint32(1 << 31)

type inlineTargetTable struct {
	first      int
	data       *inlineTargetData
	classifier wasm.ModuleInstructionClassifier
}

func (ts inlineTargetTable) resultTypes(t *inlineTarget) []machineType {
	return ts.data.types[t.localTypeEnd:t.resultTypeEnd:t.resultTypeEnd]
}

func recursiveInlineFactsCandidate(m *wasm.Module, i int, facts inlineFacts, policy CodegenPolicy) bool {
	if !recursiveInlineEnabled || policy.CompactNative || len(m.Code) != 1 || m.ImportedFuncCount() != 0 || i != 0 ||
		facts.hasControlCall || facts.hasLoop || facts.touchesMem || facts.touchesGlobal || !facts.regABIIntOnly ||
		facts.declaredLocals != 0 || facts.bodyBytes > 64 || len(facts.callees) == 0 || len(facts.callees) > 2 {
		return false
	}
	for _, callee := range facts.callees {
		if callee != 0 {
			return false
		}
	}
	return true
}

// pruneNestedSizeInlineTargets prevents transitive body omission without a
// transitive inline-local plan. If an admitted parent directly calls another
// body slated for omission, splicing only the parent would transplant that call
// into a caller that did not reserve the child's inline locals. Keep the parent
// standalone; its own compile can then inline and safely eliminate the child.
func pruneNestedSizeInlineTargets(m *wasm.Module, targets *inlineTargetTable) {
	if targets.empty() {
		return
	}
	for targetIndex := range targets.data.targets {
		target := &targets.data.targets[targetIndex]
		localIdx := target.globalIdx - targets.first
		if len(m.Code[localIdx].BodyBytes) == 0 {
			continue
		}
		r := wasm.NewReader(m.Code[localIdx].BodyBytes)
		var imm wasm.InstructionImmediate
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil || targets.classifier.ClassifyInto(r, op, &imm) != nil {
				targets.data.slots[localIdx] |= inlineTargetInvalid
				break
			}
			if imm.Kind != wasm.InstrCall {
				continue
			}
			calleeLocal := int(imm.Index) - targets.first
			if calleeLocal >= 0 && calleeLocal < len(targets.data.slots) {
				slot := targets.data.slots[calleeLocal]
				if slot != 0 && targets.data.targets[(slot&^inlineTargetInvalid)-1].omitStandalone {
					targets.data.slots[localIdx] |= inlineTargetInvalid
					break
				}
			}
		}
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
	resultTypes := f.inlineTargets.resultTypes(t)
	rN := len(resultTypes)
	fr := ctrlFrame{
		kind:        cfBlock,
		resultN:     rN,
		branchN:     uint32(rN),
		types:       resultTypes,
		res0:        t.res0,
		controlSite: -1,
		height:      f.depth(),
	}
	fr.set(ctrlRegMerge1, f.regMerge && rN == 1 && t.res0 != mtNone && t.res0 != mtV128)
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes())
	f.flush()
	f.pushCtrl(&fr)

	prevRet := f.inlineRetFrame
	f.inlineRetFrame = len(f.ctrl) - 1
	err := f.bodyLoop(wasm.NewReader(t.body), minCtrl)
	f.inlineRetFrame = prevRet
	return err
}
