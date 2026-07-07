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
// (a proxy for how much code each inline site adds). Overridable for A/B via
// WAGO_INLINE_MAXBYTES.
const inlineMaxBodyBytes = 48

// inlineCallSeqBytes is a rough per-call-site machine-code cost for the call
// sequence an inline removes (arg staging + call + result handling). Used only
// to estimate the saved bytes in the report, so an approximate constant is fine.
const inlineCallSeqBytes = 24

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
	params         int
	results        int
	regABIIntOnly  bool // signature fits the int-only register ABI
}

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

// inlineDecision applies the conservative candidate class and returns the
// verdict plus a one-line reason (why, or why not).
func inlineDecision(f inlineFacts, callSites int) (bool, string) {
	switch {
	case f.hasControlCall:
		return false, "has call_indirect/return_call"
	case f.calleeCount > 0:
		return false, fmt.Sprintf("non-leaf (%d call(s))", f.calleeCount)
	case !f.regABIIntOnly:
		return false, "signature not int-only reg-ABI"
	case f.bodyBytes > inlineMaxBytes:
		return false, fmt.Sprintf("too big (%dB > %dB)", f.bodyBytes, inlineMaxBytes)
	case callSites == 0:
		return false, "no call sites"
	default:
		loop := ""
		if f.hasLoop {
			loop = ", has loop"
		}
		return true, fmt.Sprintf("leaf, %dB, %d site(s)%s", f.bodyBytes, callSites, loop)
	}
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
		if err := wasm.ClassifyInstructionImmediateInto(r, op, &imm); err != nil {
			return err
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
			f.hasLoop = true
			scanInlineFactsAST(in.Body().Instrs, f)
		case wasm.InstrBlock:
			scanInlineFactsAST(in.Body().Instrs, f)
		case wasm.InstrIf:
			scanInlineFactsAST(in.Then(), f)
			scanInlineFactsAST(in.Else(), f)
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
