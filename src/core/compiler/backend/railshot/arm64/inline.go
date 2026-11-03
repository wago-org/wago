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

// inlineMaxBodyBytes is the default encoded-body-size ceiling for ordinary candidates
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
	boundedTuple   bool // small numeric tuple leaf, including structured loops
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
// reason. Expanded tuple candidates additionally require a call-free caller
// plan before their private register homes can be reserved.
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
	case f.inlineIntegerTuple(policy):
		return true, ""
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

func inlineOK(f inlineFacts, policy CodegenPolicy) bool {
	switch {
	case f.moduleEH:
		return false
	case policy.CompactNative:
		return compactInlineOK(f, policy)
	case f.hasControlCall, f.calleeCount > 0, !f.regABIIntOnly:
		return false
	case f.inlineIntegerTuple(policy):
		return true
	case f.hasLoop:
		return false
	case f.bodyBytes > inlineMaxBytes:
		return false
	default:
		return true
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
		f.boundedTuple = boundedIntegerTupleBody(fn, f, classifier)
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
	pairLeaf       bool // bounded parameter-only native integer pair
	tupleControl   bool // bounded integer tuple leaf with private local homes
}

const inlineRecursiveFlag = uint32(1 << 31)

func (t *inlineTarget) recursive() bool { return t.params&inlineRecursiveFlag != 0 }
func (t *inlineTarget) paramCount() int { return int(t.params &^ inlineRecursiveFlag) }

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

func (ts inlineTargetTable) target(globalIdx int) *inlineTarget {
	localIdx := globalIdx - ts.first
	if ts.data == nil || localIdx < 0 || localIdx >= len(ts.data.slots) {
		return nil
	}
	slot := ts.data.slots[localIdx]
	if slot == 0 || slot&inlineTargetInvalid != 0 {
		return nil
	}
	return &ts.data.targets[slot-1]
}

func (ts inlineTargetTable) empty() bool { return ts.data == nil || len(ts.data.targets) == 0 }

func (ts inlineTargetTable) localTypes(t *inlineTarget) []machineType {
	return ts.data.types[t.typeStart:t.localTypeEnd:t.localTypeEnd]
}

func (ts inlineTargetTable) resultTypes(t *inlineTarget) []machineType {
	return ts.data.types[t.localTypeEnd:t.resultTypeEnd:t.resultTypeEnd]
}

func (ts inlineTargetTable) omitStandaloneBody(localIdx int, hostAdapter bool) bool {
	if !inlineDeadBodyEnabled || hostAdapter {
		return false
	}
	t := ts.target(ts.first + localIdx)
	return t != nil && t.omitStandalone
}

// buildInlineTargets returns the straight-line leaf inline candidates keyed by
// GLOBAL function index, or an empty table when inlining is disabled. Candidacy
// here is a property of the callee alone (the call-site count only mattered for the report),
// so a call to one of these can be spliced wherever it appears.
func buildInlineTargets(m *wasm.Module, allHints []funcHints, policy CodegenPolicy) inlineTargetTable {
	if !policy.EnabledOption(optInline) {
		return inlineTargetTable{}
	}
	hasCall := false
	for i := range allHints {
		if allHints[i].flags.has(hintHasCall) {
			hasCall = true
			break
		}
	}
	if !hasCall {
		return inlineTargetTable{}
	}
	importedFuncs := m.ImportedFuncCount()
	if uint64(len(m.Code)) > uint64(^uint32(0)) {
		return inlineTargetTable{}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	candidateCount, typeCount := 0, 0
	for i := range m.Code {
		ft, _, ok := inlineTargetFacts(m, allHints, i, policy, classifier)
		if !ok {
			continue
		}
		candidateCount++
		add := int(allHints[i].localCount) + len(ft.Results)
		if add < 0 || typeCount > int(^uint32(0))-add {
			// Inlining is optional. Fall back to direct calls instead of retaining a
			// sidecar whose compact 32-bit ranges cannot represent this module.
			return inlineTargetTable{}
		}
		typeCount += add
	}
	if candidateCount == 0 || candidateCount >= int(inlineTargetInvalid) {
		return inlineTargetTable{}
	}
	data := &inlineTargetData{
		slots:   make([]uint32, len(m.Code)),
		targets: make([]inlineTarget, 0, candidateCount),
		types:   make([]machineType, 0, typeCount),
	}
	targets := inlineTargetTable{first: importedFuncs, data: data, classifier: classifier}
	for i := range m.Code {
		ft, facts, ok := inlineTargetFacts(m, allHints, i, policy, classifier)
		if !ok {
			continue
		}
		h := allHints[i]
		localStart := len(data.types)
		for _, p := range ft.Params {
			data.types = append(data.types, mtOf(p))
		}
		for _, run := range m.Code[i].Locals.Runs {
			for k := 0; k < int(run.Count); k++ {
				data.types = append(data.types, mtOf(run.Type))
			}
		}
		localEnd := len(data.types)
		for _, result := range ft.Results {
			data.types = append(data.types, mtOf(result))
		}
		resultEnd := len(data.types)
		res0 := mtNone
		if len(ft.Results) > 0 {
			res0 = data.types[localEnd]
		}
		params := uint32(facts.params)
		if recursiveInlineCandidate(m, allHints, i, facts, policy) {
			params |= inlineRecursiveFlag
		}
		data.targets = append(data.targets, inlineTarget{
			body:           m.Code[i].BodyBytes,
			globalIdx:      importedFuncs + i,
			localDeclBytes: m.Code[i].LocalDeclBytes,
			typeStart:      uint32(localStart),
			localTypeEnd:   uint32(localEnd),
			resultTypeEnd:  uint32(resultEnd),
			params:         params,
			res0:           res0,
			touchesMem:     facts.touchesMem,
			touchesGlob:    facts.touchesGlobal,
			hasCtrl:        facts.hasControlFlow,
			omitStandalone: policy.CompactNative &&
				h.inlineCallSites == 1 && h.directCallRefs == 1 && !h.flags.has(hintHasInlineLoopCall),
			pairLeaf: facts.regABIIntOnly && facts.params <= 4 && facts.results == 2 && facts.declaredLocals == 0 &&
				facts.calleeCount == 0 && !facts.hasControlFlow && !facts.touchesMem && !facts.touchesGlobal && len(m.Code[i].BodyBytes) <= 16 &&
				shared.IsStraightLineBlock(wasm.NewReader(m.Code[i].BodyBytes), targets.classifier),
			tupleControl: (facts.hasLoop || facts.bodyBytes > inlineMaxBytes) && facts.inlineIntegerTuple(policy),
		})
		data.slots[i] = uint32(len(data.targets))
	}
	if policy.CompactNative {
		pruneNestedSizeInlineTargets(m, &targets)
	}
	return targets
}

func inlineTargetFacts(m *wasm.Module, allHints []funcHints, i int, policy CodegenPolicy, classifier wasm.ModuleInstructionClassifier) (*wasm.CompType, inlineFacts, bool) {
	body := m.Code[i].BodyBytes
	if len(body) == 0 {
		return nil, inlineFacts{}, false
	}
	ft, ok := m.LocalFuncType(i)
	if !ok || ft == nil {
		return nil, inlineFacts{}, false
	}
	h := allHints[i]
	if h.flags.has(hintModuleEH) {
		return nil, inlineFacts{}, false
	}
	facts := inlineFacts{
		bodyBytes:      len(body),
		hasLoop:        h.flags.has(hintHasLoop),
		hasControlFlow: h.flags.has(hintHasControlFlow),
		touchesGlobal:  h.globalCount != 0,
		touchesMem:     h.flags.has(hintTouchesMemory | hintUsesBulkMem),
		params:         len(ft.Params),
		results:        len(ft.Results),
		declaredLocals: int(h.localCount) - len(ft.Params),
		callSites:      int(h.inlineCallSites),
		regABIIntOnly:  sigFitsRegABI(ft) && sigIsIntOnly(ft),
	}
	if h.flags.has(hintHasCall) {
		facts.calleeCount = 1
	}
	facts.boundedTuple = boundedIntegerTupleBody(m.Code[i], facts, classifier)
	return ft, facts, inlineOK(facts, policy) || recursiveInlineCandidate(m, allHints, i, facts, policy)
}

func recursiveInlineCandidate(m *wasm.Module, allHints []funcHints, i int, facts inlineFacts, policy CodegenPolicy) bool {
	if !recursiveInlineEnabled || policy.CompactNative || len(m.Code) != 1 || m.ImportedFuncCount() != 0 || i != 0 {
		return false
	}
	h := allHints[i]
	return h.flags.has(hintCallsSelf) && !h.flags.has(hintHasLoop|hintTouchesMemory|hintUsesBulkMem|hintHasNonDirectCall|hintCallsImport|hintModuleEH) &&
		facts.regABIIntOnly && facts.declaredLocals == 0 && facts.bodyBytes <= 64 && h.callRelocSiteCount() <= 2
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

// inlineInLoopIsRegressive identifies a tiny stateless leaf whose native
// register-ABI call is cheaper than repeatedly materializing the inliner's
// synthetic local area in a caller loop. Keep the direct call in that case; it
// still benefits from the call-preserving leaf ABI in call.go. Bounded integer
// pairs instead retain private parameters or forward read-only caller locals.
func (t *inlineTarget) inlineInLoopIsRegressive() bool {
	return int(t.localTypeEnd-t.typeStart) == t.paramCount() && !t.touchesMem && !t.touchesGlob && !t.hasCtrl && !t.integerPairLeaf() && !t.tupleControl
}

// integerPairLeaf is a bounded, parameter-only integer tuple helper. Inline
// targets already have integer register-ABI signatures and no ordinary calls.
// Its private parameters can use otherwise idle caller pins instead of forcing
// every argument and returned local reference through the frame in a loop.
func (t *inlineTarget) integerPairLeaf() bool {
	return t.pairLeaf
}

// reserveInlineLocals scans the caller body for calls to inline targets and, for
// each distinct spliced callee, reserves its params+locals as fresh frame locals
// PAST f.nLocals (so the prologue's zeroDeclaredLocals — bounded by f.nLocals —
// never touches them; each splice binds/zeroes them itself). Records the base for
// callOp. Must run after assignPinnedLocals (which sizes f.locals): the reserved
// locals normally remain unpinned; bounded integer tuples can use spare pins in
// a call-free caller that does not advertise preserving its caller's pins.
// All splice sites share one region. setLocal preserves outstanding references
// before rebinding it, and declared-local callees eagerly realize their results.
func (f *fn) reserveInlineLocals(callees []*inlineTarget, targets inlineTargetTable) {
	if len(callees) == 0 {
		return
	}
	f.inlineTargets = targets
	if len(callees) <= maxRetainedInlineBases {
		clear(f.inlineBasePool)
		if f.inlineBasePool == nil {
			f.inlineBasePool = make(map[int]int, len(callees))
		}
		f.inlineBase = f.inlineBasePool
	} else {
		f.inlineBase = make(map[int]int, len(callees))
	}
	for _, t := range callees {
		if t.tupleControl && f.makesCalls {
			continue
		}
		base := len(f.localType)
		for _, lt := range targets.localTypes(t) {
			f.localType = append(f.localType, lt)
			// compileFuncAttempt rejects the completed frame before any of these
			// homes are consumed. Accepted native frames are far below uint32
			// slots, so this representation is exact on every successful path.
			f.localSlot = append(f.localSlot, uint32(f.nLocalSlots))
			f.nLocalSlots += lt.stackSlots()
			f.locals = append(f.locals, localDef{reg: regNone, state: lsMem})
		}
		f.inlineBase[t.globalIdx] = base
		if (t.integerPairLeaf() || t.tupleControl) && !f.makesCalls && !f.preserveCallerPins && !f.policy.CompactNative {
			// Respect the caller's fixed roles and existing pins, including the
			// scalar merge register. Preserve the allocator's transient budget.
			var storage [24]Reg
			pool := gpPinPoolWithPolicy(storage[:0], true, f.nParams, true, f.policy)
			for i := base; i < len(f.locals); i++ {
				for _, reg := range pool {
					if reg == mergeReg || f.reserved.has(reg) || f.pinnedLocalMask.has(reg) || f.pinnedLocalMask.count() >= gpPinLimit(f.reserved) {
						continue
					}
					f.locals[i].reg = reg
					f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
					if t.tupleControl {
						f.stats.peep("inline-integer-tuple-local-pin")
					} else {
						f.stats.peep("inline-integer-pair-param-pin")
					}
					break
				}
			}
		}
	}
}

// collectInlinedCallees scans the caller body once and returns the distinct
// inline targets it calls, in first-call order. Computed before frame setup so
// the caller's guard-page pin exclusion can be re-derived from the callees
// (whether any touches memory), and reused by reserveInlineLocals.
func collectInlinedCallees(caller *wasm.Func, targets inlineTargetTable) []*inlineTarget {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return nil
	}
	var out []*inlineTarget
	var smallSeen [inlineLinearSeenTargets]int
	seenN := 0
	var largeSeen map[int]struct{}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return out
		}
		if _, ok := wasm.ImmediateFreeInstructionKind(op); ok {
			continue
		}
		var t *inlineTarget
		if op == 0x10 { // call
			idx, err := r.U32()
			if err != nil {
				return out
			}
			t = targets.target(int(idx))
		} else {
			switch op {
			case 0x05, 0x0b: // else, end
				continue
			case 0x08, 0x0c, 0x0d, 0x12, 0x14, 0x15,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
				if _, err := r.U32(); err != nil {
					return out
				}
				continue
			case 0x41:
				if _, err := r.I32(); err != nil {
					return out
				}
				continue
			case 0x42:
				if _, err := r.I64(); err != nil {
					return out
				}
				continue
			case 0x43:
				if _, err := r.Bytes(4); err != nil {
					return out
				}
				continue
			case 0x44:
				if _, err := r.Bytes(8); err != nil {
					return out
				}
				continue
			}
			if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
				return out
			}
			if imm.Kind != wasm.InstrCall {
				continue
			}
			t = targets.target(int(imm.Index))
		}
		if t == nil {
			continue
		}
		duplicate := false
		if largeSeen != nil {
			_, duplicate = largeSeen[t.globalIdx]
		} else {
			for _, globalIdx := range smallSeen[:seenN] {
				if globalIdx == t.globalIdx {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			continue
		}
		if largeSeen != nil {
			largeSeen[t.globalIdx] = struct{}{}
		} else if seenN < len(smallSeen) {
			smallSeen[seenN] = t.globalIdx
			seenN++
		} else {
			largeSeen = make(map[int]struct{}, 2*len(smallSeen))
			for _, globalIdx := range smallSeen {
				largeSeen[globalIdx] = struct{}{}
			}
			largeSeen[t.globalIdx] = struct{}{}
		}
		out = append(out, t)
	}
	return out
}

// allCallsWillInline reports whether every call in caller is a direct call that
// the current inline plan will splice. This lets frame/register planning treat
// such a caller as truly call-free: no BL remains to clobber LR or local pins.
// Calls in loops honor the same regression guard as callOp.
func allCallsWillInline(caller *wasm.Func, targets inlineTargetTable, policy CodegenPolicy) bool {
	if targets.empty() || len(caller.BodyBytes) == 0 {
		return false
	}
	r := wasm.NewReader(caller.BodyBytes)
	var imm wasm.InstructionImmediate
	var controlLoops uint64 // one bit per ordinary control frame
	controlDepth := 0
	var deepControls []bool // pathological depth beyond the fixed 64-bit stack
	loopDepth := 0
	sawCall := false
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := targets.classifier.ClassifyInto(r, op, &imm); err != nil {
			return false
		}
		switch op {
		case 0x02, 0x04: // block, if
			if controlDepth < 64 {
				controlLoops &^= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, false)
			}
			controlDepth++
		case 0x03: // loop
			if controlDepth < 64 {
				controlLoops |= uint64(1) << controlDepth
			} else {
				deepControls = append(deepControls, true)
			}
			controlDepth++
			loopDepth++
		case 0x0b: // end (the final function end has no matching entry here)
			if controlDepth != 0 {
				controlDepth--
				wasLoop := false
				if controlDepth < 64 {
					bit := uint64(1) << controlDepth
					wasLoop = controlLoops&bit != 0
					controlLoops &^= bit
				} else {
					i := controlDepth - 64
					wasLoop = deepControls[i]
					deepControls = deepControls[:i]
				}
				if wasLoop {
					loopDepth--
				}
			}
		}
		switch imm.Kind {
		case wasm.InstrCall:
			sawCall = true
			t := targets.target(int(imm.Index))
			if t == nil || t.recursive() || (loopDepth != 0 && t.inlineInLoopIsRegressive()) {
				return false
			}
		case wasm.InstrReturnCall, wasm.InstrCallIndirect, wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef:
			return false
		}
	}
	return sawCall
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
// the reserved region), then decouples results when raw declared-local zeroing
// could overwrite their sources. Small parameter-only pairs retain pure result trees;
// their next parameter binding preserves outstanding reads through setLocal.
// A straight-line callee runs on the frameless path; a control-flow callee runs
// under a synthetic boundary frame (inlineBodyCtrl).
func (f *fn) inlineCall(t *inlineTarget, caller *wasm.Reader) error {
	f.stats.call(callKindInline)
	start := f.a.Len()
	storeLocals := tupleStoreHint{locals: [2]int{-1, -1}}
	if t.tupleControl {
		storeLocals = f.inlineTupleStoreLocals(caller)
	}
	base := f.inlineBase[t.globalIdx]
	if t.integerPairLeaf() || t.tupleControl {
		// Preserve trap order and the caller's source context before entering
		// the splice. Only pure arithmetic may remain deferred across it.
		f.materializePendingTraps()
	}
	resultHomes := f.coalesceInlineTupleResultHomes(t, storeLocals)
	defer resultHomes.restore(f)
	previousForward, previousForwardN := f.inlineForward, f.inlineForwardN
	if !f.forwardIntegerPairParams(t) {
		f.bindInlineParams(t, base)
	}

	old := f.localBase
	oldTraceFunc, oldTraceBase, oldPC := f.traceFuncIdx, f.tracePCBase, f.wasmPC
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		previous := f.enterProfileInline()
		defer f.leaveProfileInline(previous)
	}
	f.localBase = base
	f.traceFuncIdx, f.tracePCBase = uint32(t.globalIdx), t.localDeclBytes
	oldInlineDepth := f.inlineDepth
	f.inlineDepth++
	var err error
	storesAssigned := false
	if t.hasCtrl {
		var selected bool
		selected, storesAssigned, err = f.inlineTupleSelectionBody(t, storeLocals, caller)
		if !selected && err == nil {
			err = f.inlineBodyCtrl(t)
		}
	} else {
		err = f.inlineBody(t.body)
	}
	if err == nil && (t.integerPairLeaf() || t.tupleControl) {
		// A dropped tuple member must still trap, at the callee's call site.
		f.materializePendingTraps()
	}
	f.inlineForward, f.inlineForwardN = previousForward, previousForwardN
	f.localBase = old
	f.inlineDepth = oldInlineDepth
	f.traceFuncIdx, f.tracePCBase, f.wasmPC = oldTraceFunc, oldTraceBase, oldPC
	if err != nil {
		return err
	}
	if !storesAssigned {
		if err := f.consumeInlineTupleStores(t, caller); err != nil {
			return err
		}
	}

	// The callee's results sit on the operand stack; a bare `local.get` result is a
	// lazy stLocalRef into a reserved slot, and a deferred result may read one. A
	// later splice of the same callee rebinds those slots, so realize any operand
	// still referencing the reserved region into a register/value now. (The control-
	// flow path already merged results into canonical slots, so this is a no-op there.)
	nLocals := int(t.localTypeEnd - t.typeStart)
	if !t.integerPairLeaf() {
		f.realizeInlineRange(base, base+nLocals)
	}
	// Parameter-only pair leaves have no raw declared-local zeroing. Every
	// future write to their private area goes through setLocal, whose
	// realizeLocalRefs preserves all outstanding references before rebinding.
	// Leave pure result trees deferred so the caller can consume them directly.
	f.stats.addInlineSiteBytes(f.a.Len() - start)
	return nil
}

// forwardIntegerPairParams removes private copies only when every argument is
// a bare read of an original caller local, and the bounded callee cannot write
// any parameter. This remaps logical reads, never aliases local register homes.
func (f *fn) forwardIntegerPairParams(t *inlineTarget) bool {
	if !t.integerPairLeaf() || t.paramCount() == 0 || f.inlineDepth != 0 || len(f.intervalLast) != 0 || f.policy.CompactNative {
		return false
	}
	r := wasm.NewReader(t.body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil || !(op == 0x01 || op == 0x0b || op == 0x1a || op == 0x1b || op == 0x20 || op >= 0x41 && op <= 0xc4) || f.classifier.ClassifyInto(r, op, &imm) != nil {
			return false
		}
	}
	var sources [4]uint32
	localTypes := f.inlineTargets.localTypes(t)
	e := f.s.back()
	for i := t.paramCount() - 1; i >= 0; i-- {
		if e == nil || e == f.s.head || e.elemKind() != ekValue || (e.st.kind != stLocalRef && e.st.kind != stLocalReg) ||
			int(e.st.idx) >= f.nLocals || e.st.typ != localTypes[i] || f.localType[e.st.idx] != localTypes[i] {
			return false
		}
		sources[i] = e.st.idx
		e = f.s.prev(e)
	}
	for range t.paramCount() {
		f.erase(f.s.back())
	}
	f.inlineForward, f.inlineForwardN = sources, uint32(t.paramCount())
	f.stats.peepN("inline-integer-pair-param-forward", t.paramCount())
	return true
}

// bindInlineParams binds the p argument operands into the callee's param locals
// and zero-initializes its declared locals (wasm zero-init, re-done each splice
// so a call in a loop or a second site always starts from zero). Each declared
// local is cleared across its full slot width (a v128 local clears both halves).
func (f *fn) bindInlineParams(t *inlineTarget, base int) {
	nLocals := int(t.localTypeEnd - t.typeStart)
	params := t.paramCount()
	// The p args are the top operands (deepest = param 0). Pop each into its param
	// local. setLocal takes the absolute index (localBase is still 0 here).
	for i := params - 1; i >= 0; i-- {
		f.setLocal(nil, base+i, false)
		if _, _, pinned := f.pinReg(base + i); pinned {
			f.locals[base+i].state = lsReg
		}
	}
	if nLocals > params {
		z := regNone
		// arm64: zero a register via MOVZ #0 (no flag side-effect, unlike x86's
		// xor-self). f.st64 hides the scaled-offset encodability fallback for the
		// frame store (§6.1: never call the raw Store64, which returns ok bool).
		localTypes := f.inlineTargets.localTypes(t)
		for i := params; i < nLocals; i++ {
			if pr, isFloat, pinned := f.pinReg(base + i); pinned && !isFloat {
				f.a.MovImm64(pr, 0)
				f.locals[base+i].state = lsReg
				f.setFactsForLocal(base+i, 0)
				continue
			}
			if z == regNone {
				z = f.allocReg(0)
				f.a.MovImm64(z, 0)
			}
			for s := 0; s < localTypes[i].stackSlots(); s++ {
				f.st64(SP, f.localOff(base+i)+int32(8*s), z)
			}
			f.locals[base+i].state = lsMem
		}
		if z != regNone {
			f.release(z)
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
	if f.inlineTupleFallsThrough(t) {
		fr.controlSite = inlineTupleFallthroughSite
		f.stats.peep("inline-tuple-fallthrough")
	}
	var prefix inlineReadonlyPrefix
	if fr.isInlineTupleFallthrough() {
		prefix = f.detachInlineReadonlyPrefix()
		fr.height = f.depth()
	}
	defer f.restoreInlineReadonlyPrefix(prefix)
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes())
	f.flush()
	f.pushCtrl(&fr)

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
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
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
		var previous profileOrigin
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			previous = f.enterProfileInstruction()
		}
		err = f.emitPlain(r, op)
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.switchProfileOrigin(previous)
		}
		if err != nil {
			return err
		}
	}
}

// realizeInlineRange forces any operand-stack entry that references a reserved
// inline local in [lo, hi) into a register/value, so it no longer depends on that
// slot's contents (mirrors realizeLocalRefs, over a range).
func (f *fn) realizeInlineRange(lo, hi int) {
	inRange := func(idx int) bool { return idx >= lo && idx < hi }
	for e := f.s.next(f.s.head); e != f.s.head; {
		next := f.s.next(e)
		switch {
		case e.elemKind() == ekValue && (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && inRange(e.st.index()):
			f.materializeByType(e)
		case e.elemKind() == ekValue && e.st.kind == stMemRef && (inRange(e.st.memBorrow()) || inRange(e.st.memAliasLocal())):
			f.materializeByType(e)
		case e.elemKind() == ekDeferred && subtreeRefsLocalRange(f.s, e, lo, hi):
			f.condense(e, regNone)
		}
		e = next
	}
}

// subtreeRefsLocalRange reports whether the valent block rooted at e reads any
// local in [lo, hi).
func subtreeRefsLocalRange(s *stack, e *elem, lo, hi int) bool {
	if e == nil {
		return false
	}
	if e.elemKind() == ekValue {
		return (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.index() >= lo && e.st.index() < hi
	}
	if e.elemKind() == ekDeferred {
		return subtreeRefsLocalRange(s, s.arg0(e), lo, hi) || subtreeRefsLocalRange(s, s.arg1(e), lo, hi)
	}
	return false
}
