//go:build arm64

package arm64

import (
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"

	"encoding/binary"
	"errors"
	"fmt"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"os"
	"runtime"
	"sort"

	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) opt(option optimization.Option) bool {
	return f.policy.Selection.EnabledResolvedOption(option)
}

func gpCandBefore(a, b gpCand) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.global != b.global {
		return !a.global // tie: prefer a local value over a global pointer
	}
	return a.idx < b.idx
}

func insertGPCandidate(top []gpCand, candidate gpCand, limit int) []gpCand {
	position := len(top)
	for i := range top {
		if gpCandBefore(candidate, top[i]) {
			position = i
			break
		}
	}
	if position >= limit {
		return top
	}
	if len(top) < limit {
		top = append(top, gpCand{})
	}
	copy(top[position+1:], top[position:len(top)-1])
	top[position] = candidate
	return top
}

func insertLocalCandidate(top []uint16, candidate uint16, scores []uint32, limit int) []uint16 {
	position := len(top)
	candidateScore := localHotness(scores[candidate])
	for i, local := range top {
		score := localHotness(scores[local])
		if candidateScore > score || candidateScore == score && candidate < local {
			position = i
			break
		}
	}
	if position >= limit {
		return top
	}
	if len(top) < limit {
		top = append(top, 0)
	}
	copy(top[position+1:], top[position:len(top)-1])
	top[position] = candidate
	return top
}

func align16(n int) int { return (n + 15) &^ 15 }

func alignmentPadding(off int, log2 uint8) int {
	if log2 < 2 {
		log2 = 2
	}
	align := 1 << log2
	return (-off) & (align - 1)
}

// functionStartPadding makes optional entry alignment a policy-owned, costed
// choice. Ordinary code keeps addressable entries aligned and admits statically hot
// or large bodies only when their Wasm size can amortize the exact padding.
func functionStartPadding(off, bodyBytes int, hostAdapter bool, hints funcHints, policy CodegenPolicy) int {
	flags := boolFlag(hostAdapter, layoutHostAdapter) | boolFlag(hints.flags.has(hintHasLoop), layoutHasLoop) | boolFlag(hints.flags.has(hintHasCall), layoutHasCall) | boolFlag(hints.flags.has(hintCallsSelf), layoutCallsSelf)
	return functionStartPaddingFlags(off, bodyBytes, flags, scopedFloatFunctionLayout(hints, policy), policy)
}

func scopedFloatFunctionLayout(hints funcHints, policy CodegenPolicy) bool {
	return scopedLoopFloatConstsEnabled && !policy.CompactNative && policy.EnabledOption(optScopedLoopFloatConst) &&
		hints.localCount <= pollFreeLoopPhaseMaxLocals &&
		hints.flags.has(hintHasLoop) && hints.flags.has(hintHasCall) && hints.flags.has(hintHasFloatConst) &&
		!hints.flags.has(hintModuleEH)
}

func boolFlag(on bool, flag uint8) uint8 {
	if on {
		return flag
	}
	return 0
}

func functionStartPaddingFlags(off, bodyBytes int, flags uint8, scopedFloat bool, policy CodegenPolicy) int {
	mandatory := alignmentPadding(off, 2)
	if scopedFloat {
		return alignmentPadding(off, 5)
	}
	optional := alignmentPadding(off, policy.FunctionAlignLog2)
	if optional == mandatory || policy.FunctionAlignLog2 <= 2 {
		return mandatory
	}
	if flags&layoutHostAdapter != 0 {
		return optional
	}
	if flags&(layoutHasLoop|layoutHasCall|layoutCallsSelf) == 0 && bodyBytes < 256 {
		return mandatory
	}
	budget := min(12, bodyBytes/16)
	if optional <= budget {
		return optional
	}
	return mandatory
}

func (f *fn) alignCode(log2 uint8) int {
	if log2 == 0 {
		// Isolated backend primitive tests construct fn directly. Production
		// compilation always supplies a resolved policy.
		log2 = 4
	}
	pad := alignmentPadding(f.a.Len(), log2)
	for range pad / 4 {
		f.a.Nop()
	}
	return pad
}

func asmCapForBody(bodyLen int) int {
	// A direct lowering usually emits several native bytes per wasm byte. Reserve
	// enough for small/medium functions to avoid repeated encoder slice growth,
	// but clamp so a huge wasm body cannot force a huge speculative allocation.
	const (
		minAsmCap = 128
		maxAsmCap = 64 << 10
	)
	capHint := 64 + bodyLen*4
	if capHint < minAsmCap {
		return minAsmCap
	}
	if capHint > maxAsmCap {
		return maxAsmCap
	}
	return capHint
}

// moduleNeedsAdapterBacklink reports whether functions of this module carry the
// adapter backlink: only a module with return_call_ref can tail-enter an
// internal descriptor from a wrapper-ABI caller. The dynamic-call hint bit is a
// cheap superset; without hints the backlink is emitted.
func (sc *scratch) moduleNeedsAdapterBacklink(calleeHints []funcHints) bool {
	if sc == nil || calleeHints == nil {
		return true
	}
	if !sc.adapterBacklinkKnown {
		for i := range calleeHints {
			if calleeHints[i].hasUnsupportedDynamicCall() {
				sc.adapterBacklink = true
				break
			}
		}
		sc.adapterBacklinkKnown = true
	}
	return sc.adapterBacklink
}

func (c *adapterTemplateCache) lookup(ft *wasm.CompType, memSize Reg) (code []byte, returnOff, endOff int, ok bool) {
	if c.typ != ft || c.memSize != memSize || c.n == 0 {
		return nil, 0, 0, false
	}
	return c.code[:c.n], int(c.returnOff), int(c.endOff), true
}

func (c *adapterTemplateCache) observe(ft *wasm.CompType, memSize Reg, code []byte, returnOff, endOff int) {
	if len(code) > len(c.code) || returnOff <= 4 || returnOff > len(code) || endOff > len(code) {
		c.candidate = nil
		return
	}
	if c.candidate != ft || c.candidateMemSize != memSize {
		c.candidate = ft
		c.candidateMemSize = memSize
		return
	}
	copy(c.code[:], code)
	c.typ = ft
	c.memSize = memSize
	c.n = uint16(len(code))
	c.returnOff = uint16(returnOff)
	c.endOff = uint16(endOff)
}

// scratchState keeps low-level backend tests able to exercise an isolated fn.
// Production compilation always installs the module-owned scratch explicitly.
func (f *fn) scratchState() *scratch {
	if f.sc == nil {
		f.sc = &scratch{}
	}
	return f.sc
}

// newCompileScratch defers target operand storage until the first fallback.
// The empty stack object remains available to diagnostics and worker teardown.
func newCompileScratch(stackCap int) *scratch {
	// Operand retirement bounds the common live set well below the legacy
	// 256-node first chunk. Preserve its growth boundaries for larger functions,
	// but do not reserve that high-water mark for every module worker up front.
	return &scratch{stack: &stack{}, stackCap: min(stackCap, 128), asm: &a64.Asm{}}
}

func (sc *scratch) ensureTargetStack() {
	if len(sc.stack.chunks) != 0 {
		return
	}
	sc.stack.initWithCap(sc.stackCap)
	_, reserved := sc.stack.nodeMemory()
	sc.nodeScratchReserved = reserved
	sc.nodeScratchPeak = reserved
	sc.reserveControlFrames(sc.controlCap)
	sc.controlCap = 0
}

func (sc *scratch) reserveControlFrames(capacity int) {
	if capacity <= 0 {
		return
	}
	if len(sc.stack.chunks) == 0 {
		sc.controlCap = capacity
		return
	}
	sc.ctrl = make([]ctrlFrame, 0, capacity)
	sc.controlScratchReserved = capacity
	sc.controlScratchPeak = capacity
}

func (sc *scratch) reserveLocalScratch(capacity int) {
	if capacity <= 0 {
		return
	}
	sc.fnState.localType = make([]machineType, 0, capacity)
	sc.fnState.localSlot = make([]uint32, 0, capacity)
	sc.fnState.locals = make([]localDef, 0, capacity)
}

// moduleStackArenaCap chooses the first operand-stack chunk reused across the
// serial module compile. Half the body bytes plus a density/local allowance
// estimates small functions. The saturated immediate-free count occupies header
// padding and is collected in the existing scan, without resolving more types.
// Larger modules keep the established 256-node geometric arena.
func moduleStackArenaCap(m *wasm.Module, hints []funcHints) int {
	if len(hints) != len(m.Code) || moduleHasMultiValueResults(m) {
		return defaultStackArenaCap
	}
	capHint := minStackArenaCap
	for i := range hints {
		fnCap := stackArenaCapForBody(len(m.Code[i].BodyBytes), int(hints[i].localCount)) + int(hints[i].immediateFreeOpCount())/2
		if fnCap >= defaultStackArenaCap {
			return defaultStackArenaCap
		}
		if fnCap > capHint {
			capHint = fnCap
		}
	}
	return capHint
}

func moduleHasMultiValueResults(m *wasm.Module) bool {
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			ct := &m.Types[i].SubTypes[j].Comp
			if ct.Kind == wasm.CompFunc && len(ct.Results) > 1 {
				return true
			}
		}
	}
	return false
}

// serialStackArenaCap keeps the legacy growth path when lowering may expand one
// scanned instruction into uncounted nodes. This applies to spliced inline
// bodies, extension-owned custom recipes, and GC helper argument construction.
func serialStackArenaCap(m *wasm.Module, hints []funcHints, inlineTargets inlineTargetTable, expandedLowering bool) int {
	if !inlineTargets.empty() || expandedLowering {
		return defaultStackArenaCap
	}
	return moduleStackArenaCap(m, hints)
}

func expandedStackLowering(opts CompileOptions) bool {
	return len(opts.CustomInstructions) != 0 || opts.GCTypeSubtypingRefTest || opts.GCStructHelpers || opts.GCArrayHelpers
}

// moduleControlFrameCap sizes the serial compiler's reusable control stack from
// the same one-pass bytecode hints. Zero preserves lazy allocation for
// straight-line, incomplete, AST-only, or unusually deep modules.
func moduleControlFrameCap(m *wasm.Module, hints []funcHints) int {
	if len(hints) != len(m.Code) {
		return 0
	}
	maxDepth := 0
	for i := range hints {
		if depth := hints[i].controlDepth(); depth > maxDepth {
			maxDepth = depth
		}
	}
	if maxDepth == 0 || maxDepth >= maxHintedControlFrames {
		return 0
	}
	return maxDepth + 1 // implicit function frame
}

func (sc *scratch) reset() {
	if len(sc.stack.chunks) != 0 {
		sc.stack.reset()
	}
	sc.asm.B = sc.asm.B[:0]
	sc.asm.LogicalMoveImmediates = 0
	sc.asm.CompactMoveImmediates32 = 0
	sc.asm.SingleNegativeMoves32 = 0
	sc.asm.IndexedBaseReuses = 0
	sc.asm.DominatedIndexedCandidates = 0
	sc.directPrepared = false

	sc.directPreparedBounded = false
	sc.retSiteHead = 0
	sc.ctrl = sc.ctrl[:0]
	sc.fnState.loopSetLocals = sc.fnState.loopSetLocals[:0]
	clear(sc.ctrlMerges[:cap(sc.ctrlMerges)])
	clear(sc.ctrlRoots[:cap(sc.ctrlRoots)])
	sc.ctrlRoots = sc.ctrlRoots[:0]
	for i := range sc.trapSites {
		sc.trapSites[i] = sc.trapSites[i][:0]
	}
	sc.wideTrapSources = sc.wideTrapSources[:0]
	sc.branchTargets = nil
	clear(sc.finalizerMarkers)
	sc.finalFragments = sc.finalFragments[:0]
	sc.fragmentOverflow = false
	sc.deadHoleN = 0
	sc.branchNextN = 0
	sc.singleBitTestN = 0
	sc.deadHoleOverflow = false
	sc.hasBranchTargets = false
	sc.hasPCRelative = false
}

// resolveBoundedPreparedEntries is a bounded module-finalization step over the
// call relocations already recorded by direct lowering. Starting from proven
// leaves makes recursive SCCs fail closed. Work and depth caps ensure a large
// acyclic call graph cannot retain a P for an arbitrarily long interval.
func resolveBoundedPreparedEntries(m *wasm.Module, candidates []uint64, hints []funcHints, relocs callRelocTable, table immutableTableHint) []uint64 {
	if len(candidates) == 0 {
		return nil
	}
	n := len(m.Code)
	var tableTargets []uint64
	var tableTargetsOK bool
	for i := range hints {
		if directPreparedMarked(candidates, i) && hints[i].flags.has(hintHasNonDirectCall) {
			tableTargets, tableTargetsOK = immutablePreparedTableTargets(m, table)
			break
		}
	}

	safe := make([]uint64, len(candidates))
	work := make([]uint16, n)
	depth := make([]uint8, n)
	for changed := true; changed; {
		changed = false
		for i := 0; i < n; i++ {
			if !directPreparedMarked(candidates, i) || directPreparedMarked(safe, i) || i >= len(hints) {
				continue
			}
			h := hints[i]
			if h.hasUnsupportedDynamicCall() || h.flags.has(hintHasLoop) {
				continue
			}
			bodyBytes := len(m.Code[i].BodyBytes)
			if bodyBytes == 0 || bodyBytes > maxBoundedPreparedBodyBytes {
				continue
			}
			candidateWork, candidateDepth := bodyBytes, 1
			functionRelocs := relocs.serialFunction(i)
			if relocs.results != nil {
				functionRelocs = relocs.parallelFunction(i)
			}
			admit := true
			for _, rl := range functionRelocs {
				target := int(rl.targetIndex())
				if rl.target == invalidCallRelocField || target >= n || !directPreparedMarked(safe, target) {
					admit = false
					break
				}
				candidateWork += int(work[target])
				candidateDepth = max(candidateDepth, int(depth[target])+1)
			}
			if !admit {
				continue
			}
			if h.flags.has(hintHasNonDirectCall) {
				if !tableTargetsOK {
					continue
				}
				maxTargetWork, maxTargetDepth := 0, 0
				for target := 0; target < n; target++ {
					if !directPreparedMarked(tableTargets, target) {
						continue
					}
					if !directPreparedMarked(safe, target) {
						admit = false
						break
					}
					maxTargetWork = max(maxTargetWork, int(work[target]))
					maxTargetDepth = max(maxTargetDepth, int(depth[target]))
				}
				if !admit {
					continue
				}
				// Each call_indirect opcode consumes at least two body bytes.
				// Multiplying by this conservative maximum call-site count keeps
				// the work proof valid without retaining another scan counter.
				candidateWork += maxTargetWork * (bodyBytes / 2)
				candidateDepth = max(candidateDepth, maxTargetDepth+1)
			}
			if candidateWork > maxBoundedPreparedWorkBytes || candidateDepth > maxBoundedPreparedCallDepth {
				continue
			}
			safe[i>>6] |= uint64(1) << uint(i&63)
			work[i] = uint16(candidateWork)
			depth[i] = uint8(candidateDepth)
			changed = true
		}
	}
	return safe
}

func (f *fn) frameHeaderBytes() int {
	if f.compactFrameHeader {
		return 0
	}
	return frameHdrBytes
}

// prepareCompactGCFrameHeader makes the frontend's collector-local offsets
// consume this function's final local layout. The plan retains local identities
// specifically so this rewrite is allocation-free. Fixed roots belong to the EH
// layout and keep the stable header until those records become layout-relative.
func (f *fn) prepareCompactGCFrameHeader(plan *shared.GCFrameRootPlan) bool {
	if plan == nil {
		return true
	}
	if !plan.Candidate || plan.HasFixedOffsets() {
		return false
	}
	for _, local := range plan.Locals {
		if int(local.Index) >= f.nLocals {
			return false
		}
	}
	for i := range plan.Locals {
		plan.Locals[i].Offset = uint32(f.localOff(int(plan.Locals[i].Index)))
	}
	return true
}

func (f *fn) localOff(i int) int32 { return int32(f.frameHeaderBytes() + 8*int(f.localSlot[i])) }

func (f *fn) ehFrameBytes() int {
	if f.moduleEH {
		return (f.ehTryCap*ehRecordSlots + f.ehRootCap*ehRootSlots) * 8
	}
	return 0
}

func (f *fn) spillOff(k int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + f.ehFrameBytes() + 8*k)
}

// frameSize is a multiple of 16: AArch64 requires SP 16-byte aligned at all times.
// Unlike amd64 there is no "+8 bias" — a BL writes the return address into LR
// (nothing is pushed), so a call-making function's `STP X29,X30,[SP,#-16]!` keeps
// 16-alignment and `SUB SP,SP,#frameSize` must stay a multiple of 16 to preserve
// it for our own call sites.
func (f *fn) frameSize() int {
	if f.frameElided {
		return 0
	}
	return align16(f.frameHeaderBytes() + 8*f.nLocalSlots + f.ehFrameBytes() + 8*f.maxSpill)
}

func (f *fn) elideRegisterOnlyFrame() bool {
	if f.scalarSummary.Eligible {
		// Shared register-ABI entry leaves incoming parameters in registers and
		// declared zeros deferred. Loads/stores (including control agreements)
		// mark the frame as used; spill accounting alone cannot prove this.
		if !f.compactFrameHeader || !f.singleRegResult || f.makesCalls || f.scalarFrameUsed || f.maxSpill != 0 ||
			(!f.preserveCallerPins && !f.opt(optFrameElideRegHomed)) {
			return false
		}
		f.frameElided = true
		f.stats.peep("frame-adjust-elide")
		return true
	}

	voidResult := len(f.ft.Results) == 0
	registerResult := f.singleRegResult || voidResult
	if f.moduleEH || !registerResult || f.makesCalls || f.maxSpill != 0 || len(f.localType) != f.nLocals {
		return false
	}
	// The frame reserves slots for locals and operand spills. A call-free leaf with
	// no operand spills (maxSpill==0) keeps its locals permanently in registers, so
	// none of those slots is ever touched — the SUB/ADD SP pair is dead. Two ways to
	// prove the frame is untouched:
	//   1. preserveCallerPins: no locals beyond params, so no local slots at all.
	//   2. every local is register-homed (reg != regNone) and scalar: the register
	//      allocator never spills a call-free local to its frame slot, so the
	//      reserved slots stay dead even though nLocalSlots > 0.
	// A v128 local is copied through its frame slot in the prologue, so exclude it.
	if !f.preserveCallerPins && !(f.opt(optFrameElideRegHomed) && f.allLocalsRegisterHomed()) {
		return false
	}
	f.frameElided = true
	f.stats.peep("frame-adjust-elide")
	if voidResult {
		f.stats.peep("frame-adjust-elide-void")
	}
	return true
}

// allLocalsRegisterHomed reports whether every local lives in a register for the
// whole activation (never uses its reserved frame slot). Only meaningful for
// call-free functions, where locals never leave their registers.
func (f *fn) allLocalsRegisterHomed() bool {
	if len(f.locals) < f.nLocals {
		return false
	}
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i] == mtV128 || f.locals[i].reg == regNone {
			return false
		}
	}
	return true
}

func (f *fn) patchFrameAdjusts() error {
	size := f.frameSize()
	if err := f.validateFrameSize(size); err != nil {
		return err
	}
	addSites := append(f.tailFrameSites, f.addRspAt)
	if diagnosticsEnabled && f.stats != nil {
		sites := len(addSites) + 1
		f.stats.NativeSize.FrameAdjustmentBytes += 12 * sites
		if f.opt(optSmallFrame) && size <= 4095 {
			deadPerSite := 8
			if size == 0 {
				deadPerSite = 12
			}
			f.stats.NativeSize.DeadFrameReservationBytes += deadPerSite * sites
		}
	}
	if f.opt(optSmallFrame) && size <= 4095 {
		const nop = 0xD503201F
		if size == 0 {
			f.a.PatchU32(f.subRspAt, nop)
			for _, at := range addSites {
				f.a.PatchU32(at, nop)
			}
		} else {
			f.a.PatchU32(f.subRspAt, 0xD10003FF|uint32(size)<<10) // SUB SP,SP,#size
			for _, at := range addSites {
				f.a.PatchU32(at, 0x910003FF|uint32(size)<<10) // ADD SP,SP,#size
			}
			f.stats.peep("small-frame-adjust")
		}
		for _, at := range append(addSites, f.subRspAt) {
			f.a.PatchU32(at+4, nop)
			f.a.PatchU32(at+8, nop)
		}
		return nil
	}
	f.a.PatchMovImm(f.subRspAt, uint32(size))
	for _, at := range addSites {
		f.a.PatchMovImm(at, uint32(size))
	}
	return nil
}

func (f *fn) validateFrameSize(size int) error {
	headroom := nativeFrameStackFenceHeadroom(f.makesCalls)
	if size < 0 || size > headroom {
		return fmt.Errorf("arm64: native frame %d bytes exceeds stack-fence headroom %d", size, headroom)
	}
	return nil
}

func nativeFrameStackFenceHeadroom(makesCalls bool) int {
	overhead := shared.MaxNativeInboundCallBytes
	if makesCalls {
		overhead += 16 // FP/LR record is stored before the body-frame fence check.
	}
	return shared.MaxNativeFrameBytes - overhead
}

// CompileModuleWith compiles every local function. ElideBoundsChecks elides the
// inline linear-memory bounds check, relying on a guard-page mapping + SIGSEGV
// handler (the caller must back memory with runtime guard pages).
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*a64.CompiledModule, error) {
	if !diagnosticsEnabled && (opts.Stats != nil || opts.CollectInlineReport) {
		return nil, fmt.Errorf("compiler diagnostics require -tags=wago_codegenstats or wago_profile")
	}
	if opts.UnwindMaps && !opts.Profile {
		return nil, fmt.Errorf("unwind maps require profiling")
	}
	if opts.SourceMaps && !opts.Profile {
		return nil, fmt.Errorf("source maps require profiling")
	}
	if opts.Profile && !profileEnabled {
		return nil, fmt.Errorf("profiling requires a build with -tags=wago_profile")
	}
	if opts.Profile && opts.Stats == nil {
		return nil, fmt.Errorf("arm64: profiling requires a ModuleStats destination")
	}
	var compiled *a64.CompiledModule
	var err error
	if codegen.SourceChecks {
		compiled, err = compileSourceModuleWith(m, opts)
	} else {
		compiled, err = compileModuleWith(m, opts)
	}
	runtime.KeepAlive(m)
	runtime.KeepAlive(opts)
	return compiled, err
}

func compileModuleWith(m *wasm.Module, opts CompileOptions) (*a64.CompiledModule, error) {
	if opts.SyncHostSlots == 0 {
		opts.SyncHostSlots = coreruntime.MaxHostArity
	}
	if opts.SyncHostSlots < coreruntime.MaxHostArity || opts.SyncHostSlots > coreruntime.MaxSyncHostSlots {
		return nil, fmt.Errorf("arm64: synchronous host slot capacity %d is outside %d..%d", opts.SyncHostSlots, coreruntime.MaxHostArity, coreruntime.MaxSyncHostSlots)
	}
	// This is a module invariant. Resolve it once rather than rescanning imports
	// for every function.
	opts.SyncHostCalls = opts.SyncHostCalls || opts.GCStructHelpers || opts.GCArrayHelpers || moduleUsesSyncHostCalls(m, opts.ImportBindings)
	selection, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas)
	if err != nil {
		return nil, fmt.Errorf("arm64: %w", err)
	}
	policy := shared.DefaultCodegenPolicy(selection)
	if !nativeCompactionDisabled && (opts.CompactNative || nativeCompactionEnabled) {
		policy = shared.CompactCodegenPolicy(selection)
	}
	if policy.FunctionAlignLog2 < 2 {
		policy.FunctionAlignLog2 = 2
	}
	if policy.InternalAlignLog2 < 2 {
		policy.InternalAlignLog2 = 2
	}
	if policy.LoopAlignLog2 < 2 {
		policy.LoopAlignLog2 = 2
	}
	guardMode := opts.ElideBoundsChecks
	// P6.1 elision is on unless disabled per-compile (opts) or globally (env).
	boundsFacts := policy.EnabledOption(optBoundsFacts) && !opts.NoBoundsFacts
	n := len(m.Code)
	workers := shared.ResolveWorkers(opts.Workers, n, runtime.GOMAXPROCS(0))
	entry, internalEntry := shared.ModuleEntries(n)
	importedFuncs := m.ImportedFuncCount()
	nGlobals := m.GlobalCount()
	var hintStart time.Time
	if diagnosticsEnabled && (opts.Stats != nil || explainEnabled) {
		hintStart = time.Now()
	}
	allHints, hintSidecar, globalScores, streamed := takeValidationHints(m, opts.ValidationAnalysis, policy, workers, len(opts.CustomInstructions) != 0 || diagnosticsEnabled && (opts.Stats != nil || explainEnabled))
	if !streamed {
		allHints, hintSidecar, globalScores, err = computeModuleHintsWithWorkersResidencyPolicy(m, nGlobals, importedFuncs, workers, policy, diagnosticsEnabled && (opts.Stats != nil || explainEnabled))
	}
	if err != nil {
		return nil, fmt.Errorf("arm64: %w", err)
	}
	relocCap := 0
	for i := range allHints {
		relocCap += int(allHints[i].callRelocSiteCount())
	}
	if relocCap < minPreallocatedCallRelocs {
		relocCap = 0
	}
	var hintNanos uint64
	if diagnosticsEnabled && !hintStart.IsZero() {
		hintNanos = uint64(time.Since(hintStart))
	}
	immutableTable := computeImmutableTableHint(m, allHints, policy)
	modGlobals := pickModuleGlobals(m, nGlobals, globalScores)
	hostAdapters, err := shared.HostAdapterSet(m)
	if err != nil {
		return nil, fmt.Errorf("arm64: host adapter analysis: %w", err)
	}
	// A wrapper-ABI caller tail-enters a register-ABI target through its
	// offset-zero adapter. When that target tails back to a wrapper, it discards
	// the adapter record, so mixed register/wrapper tail cycles stay stack
	// bounded. directCallRefs counts call and return_call references and
	// inlineCallSites only calls; a saturated count conservatively keeps one.
	for i := range hostAdapters {
		if refs := allHints[i].directCallRefs; refs == ^uint8(0) || uint16(refs) > allHints[i].inlineCallSites {
			hostAdapters[i] = true
		}
	}
	if policy.EnabledOption(optRegABI) {
		// A wrapper-ABI caller can tail-enter any exact-funcref result target.
		// Retain offset-zero adapters for this staged register-ABI class so mixed
		// wide-caller/narrow-target tails still write through the caller's X3
		// destination instead of returning an unconsumed X0 value.
		for i := range m.Code {
			ft, ok := m.FuncSignature(uint32(importedFuncs + i))
			if ok && sigFitsReferenceResultRegABI(ft) {
				hostAdapters[i] = true
			}
		}
	}
	if len(opts.CustomInstructions) != 0 {
		for i := range hostAdapters {
			hostAdapters[i] = true
		}
	}
	// Stats collection is opt-in: an explicit sink (opts.Stats) or WAGO_EXPLAIN=1.
	// nil ms => st stays nil in the loop => zero-overhead counter no-ops.
	var ms *ModuleStats
	if diagnosticsEnabled && opts.Stats != nil {
		ms = opts.Stats
	} else if diagnosticsEnabled && explainEnabled {
		ms = &ModuleStats{}
	}
	if diagnosticsEnabled && ms != nil {
		hintHeaderBytes, hintSidecarBytes := funcHintStorageBytes(allHints, hintSidecar)
		*ms = ModuleStats{
			Funcs:            make([]*CodegenStats, n),
			ModuleGlobalPins: moduleGlobalPinInfos(modGlobals),
			Compile: shared.CompileResourceStats{
				HintHeaderBytes:  hintHeaderBytes,
				HintSidecarBytes: hintSidecarBytes,
			},
		}
		ms.Compile.StageNanos[shared.CompileStageHints] = hintNanos
		if diagnosticsEnabled && (opts.CollectInlineReport || explainEnabled) {
			// Inline-candidate detection is report-only. Failure to analyze is
			// non-fatal because it never changes code generation.
			if rep, ierr := analyzeInlineCandidates(m, policy); ierr == nil {
				ms.Inline = rep
			}
		}
	}
	// Compile scratch reused across every function in the module. The operand
	// stack arena and the occurrence-tracking refs map are per-function scratch
	// that never outlives a function's compile, so resetting them (rather than
	// allocating fresh per function) removes the largest compile allocations.
	// Compile is sequential, so sharing one scratch is safe.
	// Auto-inlining (WAGO_INLINE): the straight-line leaf callees to splice at their
	// call sites, keyed by global func index. Empty when inlining is disabled.
	inlineTargets := buildInlineTargets(m, allHints, policy)
	for i := range allHints {
		ft, ok := m.LocalFuncType(i)
		if ok {
			allHints[i].flags.assign(hintPreservesCallerPins, preservesCallerPins(ft, int(allHints[i].localCount), allHints[i]))
		}
	}
	// AArch64 lowering is close to four native bytes per Wasm opcode plus
	// adapters/alignment. Reserve once so large modules do not repeatedly copy the
	// accumulated native image as it grows.
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	// Retain enough headroom for dense large images: a 3x estimate forced
	// another allocation and copy of esbuild's entire code buffer.
	codeCap := shared.TaperedModuleCodeCapacity(totalBody, n, 32, 28, 768<<10)
	moduleTypes := buildModuleTypeCache(m, totalBody)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	// Small-function modules avoid worker-result bookkeeping with serial staging.
	// Other deferred single-worker modules reuse their arena for the joined image.
	serialHeap := opts.DeferCodeMapping && (len(m.Code) == 1 && totalBody < 2<<10 ||
		len(m.Code) >= 32 && totalBody/len(m.Code) < 2<<10)
	requiresLSE := false
	if workers <= 1 && (!opts.DeferCodeMapping || serialHeap) {
		relocs := newCallRelocTable(n, relocCap)
		// Keep the serial compiler as a distinct fast path: one reusable scratch,
		// no goroutines, channels, atomics, worker metadata, or intermediate arena.
		expandedLowering := expandedStackLowering(opts)
		sc := newCompileScratch(serialStackArenaCap(m, allHints, inlineTargets, expandedLowering))
		sc.classifier = classifier
		sc.moduleTypes = moduleTypes
		sc.reserveLocalScratch(serialLocalScratchCapacity(allHints, inlineTargets, hostAdapters))
		if ctrlCap := moduleControlFrameCap(m, allHints); ctrlCap != 0 {
			sc.reserveControlFrames(ctrlCap)
		}
		var codeBuffer *coreruntime.CodeBuffer
		var err error
		if opts.DeferCodeMapping {
			codeBuffer, err = coreruntime.NewHeapCodeBuffer(codeCap)
		} else {
			codeBuffer, err = coreruntime.NewCodeBuffer(codeCap)
		}
		if err != nil {
			return nil, fmt.Errorf("arm64: allocate code image: %w", err)
		}
		keepCodeBuffer := false
		defer func() {
			if !keepCodeBuffer {
				_ = codeBuffer.Close()
			}
		}()
		if regallocCheckEnabled {
			shared.SetSourceWorkerContext(&sc.scalar, codegen.SourceContextFor(opts.Codegen, m), 0, 1)
		}
		pressureDone := false
		var directPrepared, directPreparedBounded, gcReducedBounded, gcI31Inductions []uint64
		var gcDeadBatchLayouts []uint64
		var adapterTails []adapterTailInfo
		var adapters []sharedAdapterInfo
		if policy.CompactNative {
			if policy.EnabledOption(optSharedAdapters) {
				adapters = make([]sharedAdapterInfo, 0, countHostAdapters(hostAdapters))
			} else {
				adapterTails = make([]adapterTailInfo, 0, countHostAdapters(hostAdapters))
			}
		}
		var trapBodyCluster sharedTrapBodyCluster
		pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
		for i := range m.Code {
			var st *CodegenStats
			if diagnosticsEnabled && ms != nil {
				st = &CodegenStats{RecordSources: profileEnabled && opts.Profile && opts.SourceMaps, FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
				ms.Funcs[i] = st
			}
			if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
				st.peep("inline-dead-body")
				if !relocs.appendFunction(i, nil) {
					return nil, fmt.Errorf("arm64: module call relocations exceed 32-bit range")
				}
				continue
			}
			// Align and reserve before lowering so the assembler can emit straight
			// into the module-owned image. If an unusually large function outgrows
			// the mapping tail, CommitTail rejects the detached slice and Append
			// preserves the old capacity-underestimate fallback.
			if pad := functionStartPadding(len(codeBuffer.Bytes()), len(m.Code[i].BodyBytes), hostAdapters[i], allHints[i], policy); pad != 0 {
				if err := codeBuffer.AppendZeros(pad); err != nil {
					return nil, fmt.Errorf("arm64: grow code image: %w", err)
				}
			}
			code := codeBuffer.Bytes()
			entry[i] = len(code)
			tail, err := codeBuffer.AppendTail(asmCapForBody(len(m.Code[i].BodyBytes)))
			if err != nil {
				return nil, fmt.Errorf("arm64: grow code image: %w", err)
			}
			sc.asm.B = tail
			hints := hintSidecar.viewAt(allHints[i], i)
			fnCode, rl, internalOff, err := compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, modGlobals, &hints, immutableTable, opts.ImportBindings, opts.SyncHostCalls, opts.SyncHostSlots, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.GCFrameRoots.Function(i), opts.CustomInstructions, st, inlineTargets, allHints, policy, sc)
			if err != nil {
				return nil, fmt.Errorf("arm64: function %d: %w", i, err)
			}
			requiresLSE = requiresLSE || sc.fnState.requiresLSE
			internalEntry[i] = len(code) + internalOff
			if adapterTails != nil && sc.fnState.adapterReturnOff != 0 {
				if info := sc.fnState.adapterTailInfo(); info.returnOff != 0 {
					info.function = uint32(i)
					adapterTails = append(adapterTails, info)
				}
			}
			if adapters != nil {
				if info := sc.fnState.sharedAdapterInfo(); info.endOff != 0 {
					info.function = uint32(i)
					adapters = append(adapters, info)
				}
			}
			if hostAdapters[i] {
				trapBodyCluster.reset()
			}
			if policy.EnabledOption(optSharedTrapBody) && policy.CompactNative {
				fnCode = trapBodyCluster.share(codeBuffer.Bytes(), fnCode, entry[i], sc.fnState.sharedTrapBodyInfo(), st)
			}
			if sc.directPrepared {
				directPrepared = markDirectPrepared(directPrepared, n, i)
			}

			if sc.fnState.gcDeadBatchLayout != 0 {
				if gcDeadBatchLayouts == nil {
					gcDeadBatchLayouts = make([]uint64, n)
				}
				gcDeadBatchLayouts[i] = sc.fnState.gcDeadBatchLayout
			}
			if sc.fnState.gcI31Induction {
				gcI31Inductions = markDirectPrepared(gcI31Inductions, n, i)
			}
			if sc.fnState.gcReducedBounded {
				gcReducedBounded = markDirectPrepared(gcReducedBounded, n, i)
			}
			if sc.directPreparedBounded {
				directPreparedBounded = markDirectPrepared(directPreparedBounded, n, i)
			}
			if !relocs.appendFunction(i, rl) {
				return nil, fmt.Errorf("arm64: module call relocations exceed 32-bit range")
			}
			if !codeBuffer.CommitTail(fnCode) && !codeBuffer.AdoptHeapImage(fnCode) {
				if err := codeBuffer.Append(fnCode); err != nil {
					return nil, fmt.Errorf("arm64: grow code image: %w", err)
				}
			}
			if !pressureDone && opts.MemoryPressure != nil && len(codeBuffer.Bytes()) >= pressureAt {
				pressureDone = true
				opts.MemoryPressure()
			}
		}
		moduleOther := 0
		if adapters != nil {
			var sharedBytes int
			sharedBytes, err = shareAdaptersCodeBuffer(codeBuffer, entry, internalEntry, &relocs, adapters, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
			moduleOther += sharedBytes
		} else if adapterTails != nil {
			var sharedBytes int
			sharedBytes, err = shareAdapterTailsCodeBuffer(codeBuffer, entry, internalEntry, &relocs, adapterTails, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
			moduleOther += sharedBytes
		}
		if needsGCObjectStub(&relocs) {
			relocs.gcObjectEntry = len(codeBuffer.Bytes())
			tail, err := codeBuffer.AppendTail(256)
			if err != nil {
				return nil, fmt.Errorf("arm64: grow checked-object leaf: %w", err)
			}
			stub := emitGCObjectStub(tail)
			if !codeBuffer.CommitTail(stub) {
				return nil, fmt.Errorf("arm64: commit checked-object leaf")
			}
			moduleOther += len(stub)
		}
		code := codeBuffer.Bytes()
		if err := finalizeOmittedInlineEntries(entry, internalEntry, &relocs, hostAdapters, inlineTargets); err != nil {
			return nil, err
		}
		finalizeModuleNativeSize(ms, len(code), moduleOther, len(codeBuffer.Mapping()))
		if profileEnabled && opts.Profile {
			recordProfileRegions(ms, entry, importedFuncs, len(code), len(code)-moduleOther, moduleOther)
		}
		if err := patchCallRelocs(code, entry, internalEntry, &relocs); err != nil {
			return nil, err
		}
		directPreparedBounded = mergeGCReducedBounds(resolveBoundedPreparedEntries(m, directPreparedBounded, allHints, relocs, immutableTable), gcReducedBounded)
		ms.setNodeScratchStats(sc)
		ms.finalizeCompileResourceStats()
		if diagnosticsEnabled && explainEnabled && ms != nil {
			fmt.Fprint(os.Stderr, ms.String())
		}
		if opts.DeferCodeMapping {
			code, err = codeBuffer.TakeHeap()
			if err != nil {
				return nil, fmt.Errorf("arm64: transfer heap code image: %w", err)
			}
			return &a64.CompiledModule{GCDeadInductionLayouts: gcDeadBatchLayouts, GCI31Inductions: gcI31Inductions, RequiresARM64LSE: requiresLSE, Code: code, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared, DirectPreparedBounded: directPreparedBounded, PreparedIsolatedTables: immutableTable.local}, nil
		}
		keepCodeBuffer = true
		return &a64.CompiledModule{GCDeadInductionLayouts: gcDeadBatchLayouts, GCI31Inductions: gcI31Inductions, RequiresARM64LSE: requiresLSE, Code: code, CodeImage: codeBuffer, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared, DirectPreparedBounded: directPreparedBounded, PreparedIsolatedTables: immutableTable.local}, nil
	}

	return compileModuleParallel(m, opts, workers, codeCap, entry, internalEntry, allHints, hintSidecar, immutableTable, modGlobals, hostAdapters, inlineTargets, moduleTypes, policy, ms, guardMode, boundsFacts, importedFuncs)
}

func serialLocalScratchCapacity(allHints []funcHints, inlineTargets inlineTargetTable, hostAdapters []bool) int {
	maxLocals := 0
	for i := range allHints {
		if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
			continue
		}
		// localCount is the complete parameter-plus-declared-local population.
		maxLocals = max(maxLocals, int(allHints[i].localCount))
	}
	return maxLocals
}

// finalizeOmittedInlineEntries closes the module-layout seam for standalone
// bodies proved unreachable by compact inlining. Any surviving relocation fails
// closed. Entry metadata remains structurally valid by aliasing omitted logical
// functions to one retained internal entry; the proof guarantees it is never
// observed by Wasm or the host.
func finalizeOmittedInlineEntries(entry, internalEntry []int, relocs *callRelocTable, hostAdapters []bool, targets inlineTargetTable) error {
	if len(entry) == 0 {
		return nil
	}
	anchor := -1
	for i := range entry {
		if !targets.omitStandaloneBody(i, hostAdapters[i]) {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return fmt.Errorf("arm64: every local function was marked as an omitted inline body")
	}
	for caller := 0; caller < relocs.functions(); caller++ {
		functionRelocs := relocs.serialFunction(caller)
		if relocs.results != nil {
			functionRelocs = relocs.parallelFunction(caller)
		}
		for _, rl := range functionRelocs {
			if rl.isGCObject() {
				continue
			}
			target := int(rl.targetIndex())
			if rl.target != invalidCallRelocField && target < len(entry) && targets.omitStandaloneBody(target, hostAdapters[target]) {
				return fmt.Errorf("arm64: function %d retains relocation to omitted inline body %d", caller, rl.targetIndex())
			}
		}
	}
	alias := internalEntry[anchor]
	for i := range entry {
		if targets.omitStandaloneBody(i, hostAdapters[i]) {
			entry[i], internalEntry[i] = alias, alias
		}
	}
	return nil
}

func patchCallRelocs(code []byte, entry, internalEntry []int, relocs *callRelocTable) error {
	if len(entry) < relocs.functions() {
		return fmt.Errorf("arm64: relocation entry table has %d functions, want at least %d", len(entry), relocs.functions())
	}
	asm := &a64.Asm{B: code}
	for i := 0; i < relocs.functions(); i++ {
		base := entry[i]
		functionRelocs := relocs.serialFunction(i)
		if relocs.results != nil {
			functionRelocs = relocs.parallelFunction(i)
		}
		for _, rl := range functionRelocs {
			if rl.site() == invalidCallRelocField || base < 0 || base > len(code)-4 || uint64(rl.site()) > uint64(len(code)-base-4) {
				return fmt.Errorf("arm64: invalid relocation site %#x in function %d for %d-byte code image", rl.site(), i, len(code))
			}
			if rl.isGCObject() {
				if relocs.gcObjectEntry <= 0 || relocs.gcObjectEntry >= len(code) || !patchGCObjectCall(asm, base+int(rl.site()), relocs.gcObjectEntry, rl.target) {
					return fmt.Errorf("arm64: invalid checked-object leaf relocation in function %d", i)
				}
				continue
			}
			targetIndex := int(rl.targetIndex())
			if rl.target == invalidCallRelocField || targetIndex >= len(entry) {
				return fmt.Errorf("arm64: invalid call relocation target %d for function %d", rl.targetIndex(), i)
			}
			site := base + int(rl.site())
			target := entry[targetIndex]
			if rl.targetsInternal() {
				if targetIndex >= len(internalEntry) {
					return fmt.Errorf("arm64: missing internal entry for call relocation target %d in function %d", rl.targetIndex(), i)
				}
				target = internalEntry[targetIndex]
			}
			if !asm.PatchBranch26(site, target) {
				return fmt.Errorf("arm64 direct call relocation from function %d at %#x to function %d at %#x exceeds BL range", i, site, rl.targetIndex(), target)
			}
		}
	}
	return nil
}

func finalizeModuleNativeSize(ms *ModuleStats, codeLen, moduleOther, mappedBytes int) {
	if !diagnosticsEnabled || ms == nil {
		return
	}
	var native shared.NativeSizeReport
	native.TotalBytes = codeLen
	for _, fn := range ms.Funcs {
		if fn != nil {
			native.AddFunction(fn.NativeSize)
		}
	}
	type adapterShape struct {
		hash  uint64
		bytes int
	}
	shapes := make(map[adapterShape]struct{}) // stats-only
	for _, fn := range ms.Funcs {
		if fn == nil || fn.NativeSize.HostAdapterBytes == 0 {
			continue
		}
		native.HostAdapterCount++
		shape := adapterShape{fn.NativeSize.HostAdapterShapeHash, fn.NativeSize.HostAdapterBytes}
		if _, ok := shapes[shape]; ok {
			continue
		}
		shapes[shape] = struct{}{}
		native.HostAdapterUniqueBytes += shape.bytes
	}
	native.HostAdapterShapeCount = len(shapes)
	native.HostAdapterDuplicateBytes = native.HostAdapterBytes - native.HostAdapterUniqueBytes
	tailShapes := make(map[adapterShape]struct{}) // stats-only
	tailBytes := 0
	for _, fn := range ms.Funcs {
		if fn == nil || fn.NativeSize.HostAdapterTailBytes == 0 {
			continue
		}
		tailBytes += fn.NativeSize.HostAdapterTailBytes
		shape := adapterShape{fn.NativeSize.HostAdapterTailShapeHash, fn.NativeSize.HostAdapterTailBytes}
		if _, ok := tailShapes[shape]; ok {
			continue
		}
		tailShapes[shape] = struct{}{}
		native.HostAdapterTailUniqueBytes += shape.bytes
	}
	native.HostAdapterTailShapeCount = len(tailShapes)
	native.HostAdapterTailDuplicateBytes = tailBytes - native.HostAdapterTailUniqueBytes
	native.ModuleOtherBytes = moduleOther
	native.FunctionAlignmentBytes = codeLen - native.FunctionBytes - moduleOther
	if native.FunctionAlignmentBytes < 0 {
		native.ModuleOtherBytes = native.FunctionAlignmentBytes
		native.FunctionAlignmentBytes = 0
	}
	ms.NativeSize = native
	ms.NativeSize.SetExecutableMapping(codeLen, mappedBytes)
}

func computeModuleHintsWithWorkersResidencyPolicy(m *wasm.Module, nGlobals, importedFuncs, workers int, policy CodegenPolicy, detailedResidency bool) ([]funcHints, funcHintSidecar, []int64, error) {
	n := len(m.Code)
	allHints := make([]funcHints, n)
	totalScores := 0
	intervalLocals := 0
	intervalFunctions := 0
	eventReserve := 0
	moduleEH := m.TagCount() != 0
	storageModuleEH := moduleEH
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: unknown function type", i)
		}
		count, err := countLocals(ft.Params, m.Code[i].Locals)
		if err != nil {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		intervalStorage := intervalRegionHintStorageEligible(policy.EnabledOption(optIntervalRegionPins), len(m.Code[i].BodyBytes), count, storageModuleEH)
		scoreCount := count
		if count > 64 && !intervalStorage {
			scoreCount = 64
		}
		if scoreCount > int(^uint(0)>>1)-totalScores {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint locals overflow")
		}
		allHints[i].localCount = uint16(count)
		allHints[i].flags.assign(hintIntervalRegionStorage, intervalStorage)
		totalScores += scoreCount
		if intervalStorage {
			if count > int(^uint(0)>>1)-intervalLocals {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint interval locals overflow")
			}
			intervalLocals += count
			intervalFunctions++
			eventReserve = max(eventReserve, min(shared.LocalEventInitialCapacity, len(m.Code[i].BodyBytes)/2))
		}
	}
	if uint64(totalScores) > uint64(^uint32(0)) || uint64(intervalLocals) > uint64(^uint32(0)) {
		return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint local sidecar exceeds 32-bit index capacity")
	}
	localScores := make([]uint32, totalScores)
	// Dense storage needs no per-function index. Use sparse ranges only when
	// their index plus eligible-local payload is strictly smaller, so this
	// representation cannot increase retained last-get bytes on modules where
	// most functions qualify for interval regions.
	denseLastGets := uint64(totalScores) * 4
	sparseLastGets := uint64(intervalLocals)*4 + uint64(intervalFunctions)*8
	compactLastGets := sparseLastGets < denseLastGets
	lastGetCount := totalScores
	if compactLastGets {
		lastGetCount = intervalLocals + intervalFunctions*2
	}
	localLastGets := make([]uint32, lastGetCount)
	localEventMeta := make([]uint32, intervalFunctions*2)
	// Shadow summaries are telemetry only; ordinary codegen needs the event
	// counts above but should not reserve an array of zero diagnostic records.
	var residencyShadow []shared.ResidencyShadowEntry
	if detailedResidency {
		residencyShadow = make([]shared.ResidencyShadowEntry, intervalFunctions)
	}
	var sparseGlobals []shared.GlobalHint
	var loopIntConsts []loopIntConstHintEntry
	var agg []int64
	if nGlobals > 0 && n > 0 {
		agg = make([]int64, nGlobals)
	}
	intervalRangeAt := 0
	collectLoopIntConsts := policy.EnabledOption(optLoopIntConst)
	if parallelHintScanEligible(m, nGlobals, workers) {
		var parallelEH bool
		var err error
		sparseGlobals, loopIntConsts, intervalRangeAt, parallelEH, err = scanModuleHintsParallel(m, nGlobals, importedFuncs, workers, allHints, localScores, localLastGets, compactLastGets, intervalFunctions, localEventMeta, residencyShadow, eventReserve, detailedResidency, collectLoopIntConsts)
		if err != nil {
			return nil, funcHintSidecar{}, nil, err
		}
		moduleEH = moduleEH || parallelEH
		for _, gh := range sparseGlobals {
			agg[gh.Index] += int64(gh.Score)
		}
	} else {
		var sparseAccum shared.GlobalHintAccumulator
		eligibilityTracker := newGlobalEligibilityTracker(nGlobals)
		scoreAt := 0
		intervalAt := intervalFunctions * 2
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		var localEvents shared.LocalEventTape
		if detailedResidency {
			localEvents.Events = make([]shared.LocalEvent, 0, eventReserve)
		}
		intervalEventAt := 0
		for i := range m.Code {
			nLocals := int(allHints[i].localCount)
			intervalStorage := allHints[i].flags.has(hintIntervalRegionStorage)
			scoreCount := retainedLocalScoreCount(allHints[i])
			sparseAccum.Reset(nGlobals)
			h := funcHintsWithStorage(localScores[scoreAt : scoreAt+scoreCount])
			if compactLastGets {
				if intervalStorage {
					h.localLastGet = localLastGets[intervalAt : intervalAt+nLocals]
					localLastGets[intervalRangeAt] = uint32(scoreAt)
					localLastGets[intervalRangeAt+1] = uint32(intervalAt)
					intervalRangeAt += 2
					intervalAt += nLocals
				}
			} else {
				if intervalStorage {
					h.localLastGet = localLastGets[scoreAt : scoreAt+nLocals]
				}
			}
			h.nLocals = nLocals
			h.localCount = uint16(nLocals)
			h.localStart = uint32(scoreAt)
			if intervalStorage {
				if detailedResidency {
					localEvents.Reset(shared.LocalEventLimit)
					h.localEvents = &localEvents
				} else {
					h.countLocalEvents = true
				}
			}
			h.inlineCallSites = allHints[i].inlineCallSites
			h.directCallRefs = allHints[i].directCallRefs
			h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))
			var err error
			h, err = scanFuncBodyIntoModule(m.Code[i], nLocals, nGlobals, uint32(importedFuncs+i), m.BranchHintsForFunc(uint32(importedFuncs+i)), h, &eligibilityTracker, m, &classifier, allHints, importedFuncs, &sparseAccum, collectLoopIntConsts)
			if err != nil {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: %w", i, err)
			}
			h.inlineCallSites = allHints[i].inlineCallSites
			h.directCallRefs = allHints[i].directCallRefs
			h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))
			if intervalStorage {
				count, overflow := h.localEventCountAt, h.localEventFull
				if detailedResidency {
					count, overflow = len(localEvents.Events), localEvents.Overflow
				}
				h.setLocalEventSummary(count, overflow)
				localEventMeta[intervalEventAt*2] = h.localStart
				localEventMeta[intervalEventAt*2+1] = h.localEventMeta
				if detailedResidency {
					residencyShadow[intervalEventAt] = shared.ResidencyShadowEntry{LocalStart: h.localStart, Summary: summarizeHintResidency(&localEvents, nLocals)}
				}
				intervalEventAt++
			}
			h.flags.assign(hintIntervalRegionStorage, intervalStorage)
			if h.loopIntConstCount != 0 {
				loopIntConsts = append(loopIntConsts, loopIntConstHintEntry{function: uint32(i), bits: h.loopIntConst, types: h.loopIntConstTypes, count: h.loopIntConstCount})
			}
			scoreAt += scoreCount
			moduleEH = moduleEH || h.flags.has(hintModuleEH)
			allHints[i] = h.funcHints
			start := len(sparseGlobals)
			sparseGlobals = sparseAccum.AppendTo(sparseGlobals)
			if uint64(len(sparseGlobals)) > uint64(^uint32(0)) {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint global sidecar exceeds 32-bit index capacity")
			}
			allHints[i].globalStart = uint32(start)
			allHints[i].globalCount = uint32(len(sparseGlobals) - start)
			for _, gh := range sparseGlobals[start:] {
				agg[gh.Index] += int64(gh.Score)
			}
		}
	}
	moduleSIMD := false
	for i := range allHints {
		moduleSIMD = moduleSIMD || allHints[i].flags.has(hintModuleSIMD)
	}
	if moduleEH || moduleSIMD {
		for i := range allHints {
			allHints[i].flags.assign(hintModuleEH, moduleEH)
			allHints[i].flags.assign(hintModuleSIMD, moduleSIMD)
		}
	}
	if moduleEH && !storageModuleEH {
		localScores = compactEHLocalScores(allHints, localScores)
		localLastGets = nil
		localEventMeta = nil
		residencyShadow = nil
		intervalRangeAt = 0
	}
	return allHints, funcHintSidecar{
		localScore:             localScores,
		localLastGet:           localLastGets,
		localEventMeta:         localEventMeta,
		residencyShadow:        residencyShadow,
		loopIntConsts:          loopIntConsts,
		sparseGlobals:          sparseGlobals,
		localLastGetRangeCount: uint32(intervalRangeAt / 2),
	}, agg, nil
}

// parallelHintScanEligible bounds the extra dense global scratch retained by
// workers and leaves tiny or programmatically constructed modules on the
// allocation-minimal serial path.
func parallelHintScanEligible(m *wasm.Module, nGlobals, workers int) bool {
	if workers <= 1 || len(m.Code) < workers*2 || uint64(nGlobals)*uint64(workers) > maxParallelHintGlobalEntries {
		return false
	}
	bodyBytes := 0
	for i := range m.Code {
		if len(m.Code[i].BodyBytes) == 0 {
			return false
		}
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	return bodyBytes >= minParallelHintBodyBytes
}

// computeImmutableTableHint derives the module-owned table proof after the
// function scans have established whether any body mutates table zero.
func computeImmutableTableHint(m *wasm.Module, allHints []funcHints, policy CodegenPolicy) immutableTableHint {
	immutableLocalTable := policy.EnabledOption(optImmutableTable) &&
		m.ImportedTableCount() == 0 && len(m.Tables) == 1 && !moduleExportsTable(m)
	if immutableLocalTable {
		for i := range allHints {
			if allHints[i].flags.has(hintMutatesTable) {
				immutableLocalTable = false
				break
			}
		}
	}
	if immutableLocalTable {
		tableType, tableTyped := immutableLocalTableTypeWithPolicy(m, policy)
		return immutableTableHint{
			local:             true,
			typeKey:           tableType,
			typed:             tableTyped,
			monomorphicTarget: immutableLocalTableTarget(m),
		}
	}
	return immutableTableHint{monomorphicTarget: -1}
}

// immutableLocalTableTarget returns the sole local function stored in table 0,
// or -1 when entries may name different functions (or use expression forms the
// narrow specialization does not prove). The immutable-table preconditions are
// checked by computeImmutableTableHint before this helper is used.
func immutableLocalTableTarget(m *wasm.Module) int {
	target := -1
	// A table initializer prefills every slot with its default element, so that
	// target is also a possible non-null entry (active elements below override
	// individual slots). Fold it into the monomorphic set; a non-ref.func/-ref.null
	// initializer we cannot prove disqualifies the direct-call specialization.
	if len(m.Tables) == 1 && m.Tables[0].Init != nil {
		ee, err := wasm.ParseElementExpr(*m.Tables[0].Init)
		if err != nil {
			return -1
		}
		if !ee.Null {
			local := int(ee.FuncIndex) - m.ImportedFuncCount()
			if local < 0 || local >= len(m.Code) {
				return -1
			}
			target = local
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue
		}
		if e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			return -1
		}
		for _, idx := range e.Kind.Funcs {
			local := int(idx) - m.ImportedFuncCount()
			if local < 0 || local >= len(m.Code) {
				return -1
			}
			if target < 0 {
				target = local
			} else if target != local {
				return -1
			}
		}
	}
	return target
}

func moduleExportsTable(m *wasm.Module) bool {
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable {
			return true
		}
	}
	return false
}

func immutableLocalTableTypeWithPolicy(m *wasm.Module, policy CodegenPolicy) (uint64, bool) {
	if !policy.EnabledOption(optImmutableTableType) || len(m.Tables) != 1 || m.Tables[0].Init != nil {
		return 0, false
	}
	var want uint64
	found := false
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue // cannot reach the table without table.init, already excluded
		}
		if e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			return 0, false
		}
		for _, idx := range e.Kind.Funcs {
			typeIdx, ok := m.FuncTypeIndex(uint32(idx))
			if !ok {
				return 0, false
			}
			key, ok := m.StructuralTypeKeyChecked(typeIdx.Index)
			if !ok {
				return 0, false
			}
			if !found {
				want, found = key, true
			} else if key != want {
				return 0, false
			}
		}
	}
	return want, found
}

func pickModuleGlobals(m *wasm.Module, nGlobals int, agg []int64) []moduleGlobalPin {
	if nGlobals == 0 || len(m.Code) == 0 {
		return nil
	}
	type cand struct {
		g     int
		score int64
	}
	var cs []cand
	// A module pin is an ABI-wide reservation, not a function-local choice: every
	// function must preserve the register even when it never reads that global.
	// Demand enough aggregate reuse to amortize that opportunity cost for the
	// FIRST pin as well as later pins. Empirically this retains json-as's burst
	// globals (g2/g4/g25 = 4603/1350/737 -> K=3), while rejecting blake-as's
	// modest g11/g10/g8 candidates (133/125/98), where K=1 displaced a hot local
	// and made the compression loop about 5% slower.
	extraBar := 50 * loopWeight(1)
	minScore := extraBar
	for g := 0; g < nGlobals && g < len(agg); g++ {
		if agg[g] < minScore {
			continue
		}
		gt, ok := m.GlobalTypeByIndex(uint32(g))
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		cs = append(cs, cand{g, agg[g]})
	}
	sort.SliceStable(cs, func(a, b int) bool { return cs[a].score > cs[b].score })
	if diagnosticsEnabled && debugModGlobals {
		fmt.Fprint(os.Stderr, "wago: module-global candidates:")
		for _, c := range cs {
			fmt.Fprintf(os.Stderr, " g%d=%d", c.g, c.score)
		}
		fmt.Fprintln(os.Stderr)
	}
	// K = number of module-wide registers to spend. auto (pinGlobalK<0) applies the
	// extraBar gate for the 2nd/3rd; WAGO_PIN_GLOBAL_K forces a fixed cap (0..3),
	// bypassing the gate — for A/B measuring the adaptive choice.
	limit := len(moduleGlobalRegs)
	if pinGlobalK >= 0 && pinGlobalK < limit {
		limit = pinGlobalK
	}
	var pins []moduleGlobalPin
	for k, c := range cs {
		if k >= limit {
			break
		}
		if pinGlobalK < 0 && k >= 1 && c.score < extraBar {
			break // auto: cs is score-descending, so no later candidate clears the bar
		}
		pins = append(pins, moduleGlobalPin{global: uint32(c.g), reg: moduleGlobalRegs[k]})
	}
	if diagnosticsEnabled && debugModGlobals {
		fmt.Fprintf(os.Stderr, "wago: module-pinned globals (K=%d):", len(pins))
		for _, p := range pins {
			fmt.Fprintf(os.Stderr, " g%d→%s", p.global, regName(p.reg))
		}
		fmt.Fprintln(os.Stderr)
	}
	return pins
}

// compileFunc emits the ordinary body and an optional guarded native-import
// variant. Its target-derived transient register floor prevents optional
// whole-function pins from forcing a register-pressure retry.
func compileFunc(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints *funcHintView, immutableTable immutableTableHint, importBindings []ImportBinding, syncHostCalls bool, syncHostSlots int, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers bool, gcFrameRoots *shared.GCFrameRootPlan, customInstructions map[uint32]railcore.CustomInstruction, stats *CodegenStats, inlineTargets inlineTargetTable, calleeHints []funcHints, policy CodegenPolicy, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	var compileStart time.Time
	if diagnosticsEnabled && stats != nil {
		stats.FunctionAttempts++
		compileStart = time.Now()
		defer func() { stats.CompileNanos += uint64(time.Since(compileStart)) }()
	}
	if gcFrameRoots != nil && gcFrameRoots.Candidate {
		gcFrameRoots.Exact = true
		gcFrameRoots.ResetSafepoints()
		gcFrameRoots.ResetCallsites()
		gcFrameRoots.FrameBytes = 0
		gcFrameRoots.AdapterReturnOffset = 0
	}
	sc.asm.ReserveBranch19 = false
	code, relocs, internalOff, err = compileFuncAttempt(m, gcTypeLayouts, funcIdx, hostAdapter, guardMode, boundsFacts, interruptible, modGlobals, hints, immutableTable, importBindings, syncHostCalls, syncHostSlots, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, gcFrameRoots, customInstructions, stats, true, inlineTargets, calleeHints, policy, sc)
	if errors.Is(err, errNativeBranchRange) {
		// Retry once with expansion slots. The ordinary pass keeps small
		// functions byte-for-byte unchanged; only failing functions pay for
		// the extra slots and second compilation.
		sc.asm.ReserveBranch19 = true
		if stats != nil {
			*stats = CodegenStats{FuncIdx: stats.FuncIdx, Name: stats.Name,
				RecordSources: stats.RecordSources, FunctionAttempts: stats.FunctionAttempts + 1}
		}
		if gcFrameRoots != nil && gcFrameRoots.Candidate {
			gcFrameRoots.Exact = true
			gcFrameRoots.ResetSafepoints()
			gcFrameRoots.ResetCallsites()
			gcFrameRoots.FrameBytes = 0
			gcFrameRoots.AdapterReturnOffset = 0
		}
		code, relocs, internalOff, err = compileFuncAttempt(m, gcTypeLayouts, funcIdx, hostAdapter, guardMode, boundsFacts, interruptible, modGlobals, hints, immutableTable, importBindings, syncHostCalls, syncHostSlots, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, gcFrameRoots, customInstructions, stats, true, inlineTargets, calleeHints, policy, sc)
	}
	if err == nil && sc.fnState.nativeImportRouter > 0 {
		router := sc.fnState.nativeImportRouter
		asm := &a64.Asm{B: code}
		// The emitted self-edge keeps finalization from deleting the unresolved
		// router. Disable it before every optional specialization failure path.
		if !asm.PatchBranch19(router, router+4) {
			return nil, nil, 0, fmt.Errorf("native import router range")
		}
		if len(relocs) == 0 {
			fastScratch := newScratchWithStackCap(32)
			fastScratch.nativeImportFast = true
			fastScratch.classifier = sc.classifier
			fastScratch.moduleTypes = sc.moduleTypes
			var variantStats *CodegenStats
			if diagnosticsEnabled && stats != nil {
				variantStats = &CodegenStats{}
				stats.FunctionAttempts++
			}
			fastCode, fastRelocs, fastInternal, fastErr := compileFuncAttempt(m, gcTypeLayouts, funcIdx, false, guardMode, boundsFacts, interruptible, modGlobals, hints, immutableTable, importBindings, syncHostCalls, syncHostSlots, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, nil, customInstructions, variantStats, true, inlineTargets, calleeHints, policy, fastScratch)
			if fastErr == nil && len(fastRelocs) == 0 {
				oldLen := len(code)
				padding := alignmentPadding(oldLen, 4)
				combined := append(code, make([]byte, padding)...)
				combined = append(combined, fastCode...)
				combinedAsm := &a64.Asm{B: combined}
				if combinedAsm.PatchBranch19(router, oldLen+padding+fastInternal) {
					code = combined
					sc.asm.B = combined
					if diagnosticsEnabled && stats != nil {
						extra := len(combined) - oldLen
						stats.CodeBytes += extra
						stats.NativeSize.TotalBytes += extra
						stats.NativeSize.InternalFunctionBytes += extra
						stats.NativeSize.FrameAdjustmentBytes += variantStats.NativeSize.FrameAdjustmentBytes
						stats.NativeSize.DeadFrameReservationBytes += variantStats.NativeSize.DeadFrameReservationBytes
						stats.NativeSize.BranchFoldHoleBytes += variantStats.NativeSize.BranchFoldHoleBytes
						stats.NativeSize.StoreLoadNopBytes += variantStats.NativeSize.StoreLoadNopBytes
						stats.NativeSize.SharedTrapBodyBytes = 0
						stats.NativeSize.LiteralPoolBytes = 0
						stats.GCCodeBytes.Add(variantStats.GCCodeBytes)
						stats.GCCodeBytes.Total += padding
						stats.peep("native-import-guarded-variant")
					}
				}
			}
		}
	}

	if len(sc.stack.chunks) > 1 {
		sc.finishStackFunction()
	}
	if diagnosticsEnabled && stats != nil {
		sc.noteControlScratch()
	}
	return
}

func compileFuncAttempt(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints *funcHintView, immutableTable immutableTableHint, importBindings []ImportBinding, syncHostCalls bool, syncHostSlots int, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers bool, gcFrameRoots *shared.GCFrameRootPlan, customInstructions map[uint32]railcore.CustomInstruction, stats *CodegenStats, pinLocals bool, inlineTargets inlineTargetTable, calleeHints []funcHints, policy CodegenPolicy, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	var state *fn
	defer func() {
		// Reuse the existing unwind scope: even a disabled conditional defer
		// changes TinyGo defer lowering in ordinary builds.
		if regallocCheckEnabled && state != nil {
			state.checkEndLifetimes()
		}
		if recovered := recover(); recovered != nil {
			if os.Getenv("WAGO_DEBUG_PANIC") == "1" {
				panic(recovered)
			}
			err = state.compilerPanicError(m, funcIdx, recovered)
		}
	}()

	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return nil, nil, 0, fmt.Errorf("unknown function type")
	}
	c := &m.Code[funcIdx]
	// Module hint construction already validated and counted the parameters and
	// local runs. Reuse that result.
	nLocals := hints.nLocals

	sc.reset()
	sc.asm.DenseIdxDisp = hints.memOpCount() >= 8
	// The encoder's local instruction proof does not track branch targets.
	// Guard mode removes the intervening bounds checks, so nearby accesses can
	// straddle a control-flow join without executing the ADD that seeds X16.
	sc.asm.ReuseIndexedBase = !guardMode && policy.EnabledOption(optIndexedBaseReuse)
	sc.asm.DisableLogicalMoveImmediate = !logicalMoveImmediateEnabled ||
		!policy.CompactNative
	sc.asm.AllowSingleNegativeMove32 = policy.EnabledOption(optSingleNegativeMove32)
	sc.asm.DisableCompactMoveImmediate32 = !compactMoveImmediate32Enabled ||
		!policy.CompactNative
	sc.asm.Grow(asmCapForBody(len(c.BodyBytes)))
	globalIdx := m.ImportedFuncCount() + funcIdx
	f := &sc.fnState
	localType, localSlot, locals, globalReg := f.localType, f.localSlot, f.locals, f.globalReg
	mt0, _ := m.MemoryType(0)
	boundedMemcopy := len(c.BodyBytes) <= 4096 && hints.memOpCount() <= 128
	*f = fn{a: sc.asm, s: sc.stack, sc: sc, m: m, ft: ft, gcTypeLayouts: gcTypeLayouts, classifier: sc.classifier, transient: f.transient, traceFuncIdx: uint32(globalIdx), rootTraceFuncIdx: uint32(globalIdx), tracePCBase: c.LocalDeclBytes, customInstructions: customInstructions, nParams: len(ft.Params), nLocals: nLocals, localType: localType, localSlot: localSlot, locals: locals, globalReg: globalReg[:0], guardMode: guardMode, boundsFacts: boundsFacts, interruptible: interruptible, hasLoop: hints.flags.has(hintHasLoop), gcStructHelpers: gcStructHelpers, gcArrayHelpers: gcArrayHelpers, gcFrameRoots: gcFrameRoots, moduleEH: hints.flags.has(hintModuleEH), regMerge: policy.EnabledOption(optRegMerge), globalCellReg: regNone, memSizeReg: regNone, memLimitReg: regNone, immutableLocalTable: immutableTable.local, immutableTableType: immutableTable.typeKey, immutableTableTyped: immutableTable.typed, monomorphicTarget: immutableTable.monomorphicTarget, importBindings: importBindings, stagedTailDescriptors: true, stats: stats, policy: policy, branchHints: m.BranchHintsForFunc(uint32(globalIdx)), branchHintLocalDecl: c.LocalDeclBytes, calleeHints: calleeHints, threadedMemory0: mt0.Shared, localFactsEnabled: policy.EnabledOption(optValueFacts) && !hints.flags.has(hintHasControlFlow), loadDefinedLocals: loadDefinedLocalMaskForHints(hints), reuseOperands: policy.EnabledOption(optOperandNodeReuse), memcopyTail4: policy.EnabledOption(optMemcopyTail4) && boundedMemcopy, memcopyQPairs: policy.EnabledOption(optMemcopyQPairs) && boundedMemcopy, floatLiteralPool: policy.EnabledOption(optFPLiteralPool) && len(c.BodyBytes) <= 16<<10}
	state = f
	f.adapterBacklink = sc.moduleNeedsAdapterBacklink(calleeHints)
	if f.nParams >= 64 {
		f.localWritten = ^uint64(0)
	} else if f.nParams != 0 {
		f.localWritten = 1<<f.nParams - 1
	}
	// Relocations are transient until the module owner copies them into its flat
	// arena. Reuse one function buffer instead of allocating one backing per
	// caller; larger decoded call counts can still reserve the exact target-cost
	// threshold without retaining one buffer per function.
	f.relocs = sc.relocs[:0]
	f.floatPool = f.floatPool[:0]
	f.floatPoolSites = f.floatPoolSites[:0]
	callRelocSites := hints.callRelocSiteCount()
	if callRelocSites >= minPreallocatedCallRelocs && cap(f.relocs) < int(callRelocSites) {
		f.relocs = make([]callReloc, 0, callRelocSites)
	}
	defer func() {
		sc.ctrl = f.ctrl
		sc.relocs = f.relocs
	}()
	if f.moduleEH {
		shape, err := ehFrameShape(sc, c.BodyBytes)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("function %d: %w", funcIdx, err)
		}
		f.ehTryCap, f.ehRootCap = shape.TryRecords, shape.RootRecords
	}
	f.storeForwardOK = policy.EnabledOption(optStoreForward) && len(c.BodyBytes) <= 256 && nLocals <= 8
	if f.storeForwardOK && !profileEnabled && !f.guardMode && m.ImportedMemCount() == 0 && len(m.Memories) == 1 {
		mem := m.Memories[0]
		f.sharedStoreForward = mem.Shared && !mem.Limits.Addr64 && mem.Limits.HasMax && mem.Limits.Min == mem.Limits.Max && os.Getenv("WAGO_ARM64_SHARED_STORE_FORWARD") != "0"
	}
	f.syncHostCalls = syncHostCalls
	// This proof is consumed only by direct imported calls. A leaf or a body
	// touching memory cannot use it; reject those from the existing hints before
	// allocating a CFG for every small function in a large module.
	f.dynamicHostFast = f.opt(optDirectGoHostImport) && syncHostCalls && !f.moduleEH && hints.flags.has(hintHasCall) && !hints.flags.has(hintTouchesMemory) && m.ImportedFuncCount() != 0 && shared.BoundedHostSegments(c.BodyBytes, uint32(m.ImportedFuncCount()))
	for _, typ := range ft.Params {
		if typ.Kind() != wasm.ValNum || mtOf(typ) == mtV128 {
			f.dynamicHostFast = false
		}
	}
	for _, typ := range ft.Results {
		if typ.Kind() != wasm.ValNum || mtOf(typ) == mtV128 {
			f.dynamicHostFast = false
		}
	}
	for _, run := range c.Locals.Runs {
		if run.Type.Kind() != wasm.ValNum || mtOf(run.Type) == mtV128 {
			f.dynamicHostFast = false
		}
	}
	f.syncHostSlots = syncHostSlots
	f.gcTypeSubtypingRefTest = gcTypeSubtypingRefTest
	if f.threadedMemory0 && f.opt(optSharedMemoryBase) || !guardMode && len(m.Memories) > 0 {
		f.memSizeReg = X27 // explicit bounds: X27 = memBytes for the whole module
	}
	if cap(f.localType) < nLocals {
		f.localType = make([]machineType, nLocals)
	} else {
		f.localType = f.localType[:nLocals]
	}
	i := 0
	for _, p := range ft.Params {
		f.localType[i] = mtOf(p)
		i++
	}
	for _, run := range c.Locals.Runs {
		for k := 0; k < int(run.Count); k++ {
			f.localType[i] = mtOf(run.Type)
			i++
		}
	}
	if cap(f.localSlot) < nLocals {
		f.localSlot = make([]uint32, nLocals)
	} else {
		f.localSlot = f.localSlot[:nLocals]
	}
	for i, mt := range f.localType {
		// Validation caps a function at 65,535 locals, so even an all-v128
		// local area fits exactly in uint32 slots. Inlined scratch is checked
		// against the much smaller native-frame limit below before any home is
		// consumed.
		f.localSlot[i] = uint32(f.nLocalSlots)
		f.nLocalSlots += mt.stackSlots()
	}
	// Integer pointer loops with calls keep canonical loaded addresses in their
	// producer registers. Floating-point functions retain the load rename policy;
	// unknown upper bits still require canonicalization.
	if f.hasLoop && f.loadDefinedLocals != 0 && hints.flags.has(hintHasCall) {
		integerLocals := true
		for _, typ := range f.localType {
			if typ != mtI32 && typ != mtI64 {
				integerLocals = false
				break
			}
		}
		if integerLocals {
			f.loadDefinedLocals = 0
		}
	}
	f.canonicalI32Params = canonicalI32ParamMask(hints, f.localType, f.nParams, f.opt(optValueFacts))
	hasCall := hints.flags.has(hintHasCall)
	touchesMemory := hints.flags.has(hintTouchesMemory)
	// A private prepared entry establishes X26 and preserves the full Go
	// callee-saved set, so small integer functions need not be leaves. Keep host
	// imports on the adapter unless the native-host candidate below is proven;
	// memory-touching functions, module-pinned globals and EH retain the adapter. A module-level memory alone is harmless when this function's
	// bounded scan proves that its body never reads, writes, or grows memory.
	// A native-host candidate has a register entry, but its runtime admission
	// still requires the module-wide work proof and a proven native import.
	directPrepared := policy.EnabledOption(optPreparedDirectEntry) && policy.EnabledOption(optRegABI) && (preparedDirectIntSig(ft) || preparedDirectFloatSupported && (preparedDirectFloatSig(ft) || preparedDirectMixedSig(ft))) && !touchesMemory && len(modGlobals) == 0 && !hints.flags.has(hintModuleEH) && len(c.BodyBytes) <= 96 && nLocals <= 8 &&
		((m.ImportedFuncCount() == 0 && (m.MemCount() == 0 || !hasCall)) ||
			policy.EnabledOption(optNativeLeafHost) && policy.EnabledOption(optPreparedBoundedEntry) && m.ImportedFuncCount() == 1 && shared.BoundedNativeHostBody(c.BodyBytes, 1))
	// Auto-inlining: collect the callees this caller will splice (before the pin
	// setup below, which the plan can influence). A spliced memory-touching callee
	// runs its linear-memory ops in THIS caller's frame, so fold it into
	// touchesMemory — otherwise the guard-page pin exclusion (which drops X9/X10/X11
	// from the pool for a memory-touching call-making function) would be skipped for
	// a caller whose own body never touched memory.
	var inlinedCallees []*inlineTarget
	if hasCall && !hints.skipInlineScan {
		inlinedCallees = collectInlinedCallees(c, inlineTargets)
	}
	f.linkMemoryTraps = !profileEnabled && len(inlinedCallees) == 0
	if sites := int(hints.memOpCount()); !f.linkMemoryTraps && sites >= 16 && cap(sc.trapSites[trapMemOOB]) < sites {
		sc.trapSites[trapMemOOB] = make([]trapSite, 0, sites)
	}
	// Inlined helpers execute in the caller's register frame. Reuse the existing
	// callee hints before constructing any pin pool; the module's original hints
	// remain unchanged, and no retained inline-target state is needed.
	planningHints := *hints
	for _, callee := range inlinedCallees {
		planningHints.flags |= calleeHints[callee.globalIdx-m.ImportedFuncCount()].flags & hintUsesBulkMem
	}
	hints = &planningHints

	// Inlining direct Wasm calls cannot remove GC/table/atomic runtime helpers.
	if policy.EnabledOption(optInlineCallFree) && hasCall && !hints.flags.has(hintHasNonDirectCall) && allCallsWillInline(c, inlineTargets, policy) {
		hasCall = false
		f.stats.peep("all-calls-inlined")
	}
	if !f.threadedMemory0 && commonBoundsLimitEnabled && hints.bounds4OpCount() >= 2 && f.memSizeReg != regNone && !hasCall && hints.flags.has(hintHasControlFlow) && len(modGlobals) == 0 &&
		mt0.Limits.Min != 0 && touchesMemory {
		f.memLimitReg, f.memLimitExtent = X25, 4
		f.reserved = f.reserved.add(f.memLimitReg)
		f.stats.peep("common-bounds-limit")
		f.stats.peepN("common-bounds-limit-candidate", int(hints.bounds4OpCount()))
	}
	if inlinePlanTouchesMemory(inlinedCallees) {
		touchesMemory = true
	}
	effectiveHints := *hints
	effectiveHints.flags.assign(hintHasCall, hasCall)
	// The module bit records the original body classification for callers. This
	// function may additionally become a leaf after all of its calls inline, so
	// classify its effective post-inline hints here as before.
	f.preserveCallerPins = preservesCallerPins(ft, nLocals, effectiveHints.funcHints)
	if f.preserveCallerPins {
		// Keep this leaf out of every register a direct caller may use for a
		// pinned local or merge value. Its parameters stay in X0..X7 below; all
		// temporary work uses the ordinary caller-clobbered allocation set.
		for _, r := range pinnedLocalRegs {
			f.reserved = f.reserved.add(r)
		}
		for _, r := range [...]Reg{X24, X25, X27} {
			f.reserved = f.reserved.add(r)
		}
		for _, r := range [...]Reg{X9, X10, X11, mergeReg} {
			f.reserved = f.reserved.add(r)
		}
	}
	regABI := policy.EnabledOption(optRegABI) && (sigFitsRegABI(ft) || (f.stagedTailDescriptors && sigFitsReferenceResultRegABI(ft)))
	// A proven scalar straight-line regional leaf does not use backend scratch X17 on
	// its successful path. Cache memBytes there so X27 can hold one more hot local
	// without adding a load at every check. Trap stubs may overwrite X17 only on
	// their terminal path. Calls, control, tables, and bulk helpers retain X27.
	if !f.threadedMemory0 && f.opt(optLeafScratchMemSize) && f.memSizeReg != regNone && regABI && !hasCall && !hints.flags.has(hintHasControlFlow) &&
		!hints.flags.has(hintUsesBulkMem) && len(inlinedCallees) == 0 && len(m.Tables) == 0 &&
		intervalRegionHintStorageEligible(f.opt(optIntervalRegionPins), len(c.BodyBytes), nLocals, f.moduleEH) && f.leafScratchMemorySafe(c.BodyBytes) {
		f.memSizeReg = X17
		f.stats.peep("interval-region-scratch-memsize")
	}
	// Only wrapper-ABI code reads frResultsOff. Register-ABI adapters preserve X3
	// below the internal frame, while direct internal and tail paths return in
	// registers. EH and GC frame plans retain the established fixed layout until
	// their independently generated offset tables become header-relative.
	f.compactFrameHeader = regABI && !f.moduleEH
	if f.compactFrameHeader && !f.prepareCompactGCFrameHeader(gcFrameRoots) {
		f.compactFrameHeader = false
	}
	if f.compactFrameHeader {
		f.stats.peep("frame-header-elide")
	}
	f.scalarSummary = f.admitScalar(c)
	if f.scalarSummary.Eligible {
		pinLocals = false
	} else {
		sc.ensureTargetStack()
	}
	var gpPoolStorage [24]Reg
	gpPool := gpPinPoolWithPolicy(gpPoolStorage[:0], regABI, f.nParams, !hasCall, policy)
	if f.memLimitReg != regNone {
		gpPool = withoutReg(gpPool, f.memLimitReg)
	}
	if f.moduleEH {
		// X22 carries the active handler across every local call in an
		// exception-enabled module. Cross-instance call paths save it explicitly.
		gpPool = withoutReg(gpPool, ehReg)
		f.reserved = f.reserved.add(ehReg)
	}
	if policy.EnabledOption(optLeafScratchPins) && !hasCall {
		// Keep the conservative loop exclusion and reserve X12-X14 for memory,
		// table and segment helpers (including inlined helpers). Scalar leaves
		// retain all three extra candidates.
		if !hints.flags.has(hintHasLoop | hintUsesBulkMem) {
			gpPool = append(gpPool, X12, X13)
		}
		if !hints.flags.has(hintUsesBulkMem) && len(m.Tables) == 0 {
			gpPool = append(gpPool, X14)
		}
	}
	if policy.EnabledOption(optEntryArgPins) && regABI && !hasCall {
		gpPool = append(gpPool, X2, X3, X4, X5, X6, X7)
	}
	// The inline bulk-memory helpers use X9/X10/X11 as fixed dst/src/count
	// registers after canonicalizing the operand stack. They do not participate in
	// the general allocator, so assigning a local to one of those registers would
	// let bulk memory/table helpers silently overwrite live local state (fannkuch's dynamic
	// memory.copy turned its permutation loop into an infinite loop). The pre-scan
	// records this lowering class; reserve only the colliding helper registers
	// and retain the rest of the call-free pin pool.
	if hints.flags.has(hintUsesBulkMem) {
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, X9), X10), X11)
	}
	// A call-free function can execute loop-nested scalar result joins far more
	// often than it enters or exits. Keep the canonical merge register available
	// in that class instead of spending it on one additional whole-function
	// local; this removes a spill/reload pair from each single-result join edge.
	if f.opt(optWeightedScalarMerge) && f.regMerge && !hasCall && hints.hasHotScalarMerge() {
		gpPool = withoutReg(gpPool, mergeReg)
		f.stats.peep("weighted-reg-merge")
	}
	// Memory-touching call-makers with imports or tables retain only the native
	// callee-saved pin block. Host/cross-instance/indirect setup has a wider
	// caller-clobbered surface, but X19-X25 survive those boundaries and the
	// STACK_REG model still publishes dirty values before calls. Keeping this
	// narrow block also lets hot loops remain resident when a cold import follows
	// them, matching the safe AMD64 treatment of its callee-saved pin set.
	safeColdLocalCalls := f.opt(optColdCallLocalPins) && !hints.flags.has(hintHasLoopCall) && !hints.flags.has(hintHasNonDirectCall) && !hints.flags.has(hintCallsImport)
	safeMemoryCallPins := ((hints.flags.has(hintCallsSelf) && m.ImportedFuncCount() == 0) || safeColdLocalCalls) && len(m.Tables) == 0
	if touchesMemory && hasCall && !safeMemoryCallPins {
		gpPool = callSafePinPool(gpPool)
	}
	if f.memSizeReg != regNone {
		gpPool = withoutReg(gpPool, f.memSizeReg) // X27 is the module-wide memBytes cache
		f.reserved = f.reserved.add(f.memSizeReg)
	} else if guardMode && hasCall && touchesMemory {
		// Don't pin locals to the call-scratch registers X9/X10/X11 in a
		// memory-touching, call-making function under guard-page bounds. Guard mode
		// elides the inline bounds-check code, which shifts the register liveness
		// around a call's argument staging + linMem/trap setup; a pinned local in a
		// call-scratch register is meant to be spill-managed by the STACK_REG model,
		// but in that guard-page window the staging runs out of free scratch and
		// silently corrupts the pinned value (the #144/sqlite-tokenizer register-
		// pressure class — the same one that motivated excluding the wrapper-arg
		// registers). Explicit bounds keep the check code that preserves these
		// registers here, so this is guard-page-specific. Pinning is a pure speed
		// optimization, so excluding these registers only for this class is always
		// correct. Excluding X27 instead is NOT a fix: it pushes a pin onto
		// X9/X10/X11 for other modules and reintroduces the bug.
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, X9), X10), X11)
	}
	// Cache immutable instance dispatch metadata in an otherwise
	// unused callee-saved register. Admit only import-only scalar call graphs.
	if f.opt(optNativeLeafHost) && syncHostCalls && hints.flags.has(hintCallsImport) && hints.flags.has(hintHasLoopCall) && !hints.flags.has(hintHasNonDirectCall|hintModuleEH) && len(modGlobals) == 0 && m.GlobalCount() == 0 && len(customInstructions) == 0 && len(gcTypeLayouts) == 0 && len(inlinedCallees) == 0 {
		proof := shared.BoundedHostSegments(c.BodyBytes, uint32(m.ImportedFuncCount()))
		if !proof && !guardMode && len(m.Memories) == 1 && mt0.Shared && !mt0.Limits.Addr64 && mt0.Limits.HasMax && mt0.Limits.Min == mt0.Limits.Max {
			proof, _ = shared.BoundedFixedSharedHostSegments(c.BodyBytes, uint32(m.ImportedFuncCount()), 0)
		}
		used := 0
		for _, score := range hints.localScore {
			if score > 0 {
				used++
			}
		}
		remainingPins := 0
		for _, reg := range gpPool {
			if reg >= X19 && reg <= X24 {
				remainingPins++
			}
		}
		dynamic := false
		for _, binding := range importBindings {
			if binding.Dynamic {
				dynamic = true
				break
			}
		}
		if proof && dynamic && used <= remainingPins && !f.reserved.has(X25) {
			f.cacheImportDispatch = true
			f.sharedNativePins = f.threadedMemory0 && os.Getenv("WAGO_ARM64_SHARED_NATIVE_PINS") != "0"
			gpPool = withoutReg(gpPool, X25)
			f.reserved = f.reserved.add(X25)
			f.stats.peep("import-dispatch-cache")
			if used < remainingPins && !f.reserved.has(X24) && (len(m.Memories) == 0 || f.threadedMemory0 && os.Getenv("WAGO_ARM64_SHARED_IMPORT_VARIANT") != "0") && regABI && os.Getenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT") != "0" && os.Getenv("WAGO_ARM64_NATIVE_IMPORT_TARGET_CACHE") != "0" && nativeI32ImportAliases(m) && f.nativeImportVariantEligible(c.BodyBytes) {
				f.cacheNativeImportTarget = true
				gpPool = withoutReg(gpPool, X24)
				f.reserved = f.reserved.add(X24)
				f.stats.peep("native-import-target-cache")
			}
		}
	}
	for _, mg := range modGlobals {
		gpPool = withoutReg(gpPool, mg.reg) // module-pinned global registers
		f.reserved = f.reserved.add(mg.reg)
	}
	// Leave enough unreserved registers for the widest ordinary lowering step:
	// three protected inputs/temporaries plus one result. This is a target-derived
	// bound over the allocatable file after module roles are removed, not a
	// workload heuristic. The allocator may still use these registers normally;
	// whole-function pins alone cannot consume them.
	maxPins := gpPinLimit(f.reserved)
	if len(gpPool) > maxPins {
		gpPool = gpPool[:maxPins]
	}
	// Wide local tables begin canonical so their many borrowed values cannot consume
	// the target-derived transient floor. There is no failed-attempt retry.
	if !pinLocals {
		gpPool = nil
	} else if nLocals > 64 {
		gpPool = nil
	}
	// Hot mutable-int globals share the GP pin pool with locals, holding their VALUE
	// in the register (WARP's model). In call-free functions any loop-accessed global
	// qualifies; in call-making functions only globals accessed inside a CALL-FREE
	// loop do — the spill/reload keeping the cell coherent then lands on the sparse
	// out-of-loop calls, not per iteration. Non-eligible globals use the per-run
	// cell-pointer cache (globalCellPtr).
	var globalHints []shared.GlobalHint
	// A small scalar wrapper is bounded even when its signature exceeds the
	// register ABI. The ordinary adapter only copies a capped number of slots;
	// the finalizer checks the body-work proof before publishing this bit.
	sc.directPreparedBounded = !regABI && f.opt(optPreparedBoundedEntry) && sigIsIntOnly(ft) &&
		len(ft.Params) <= 128 && len(ft.Results) <= 128 && nLocals <= 128 &&
		len(c.BodyBytes) != 0 && len(c.BodyBytes) <= maxBoundedPreparedBodyBytes && !f.hasLoop &&
		!hints.flags.has(hintHasCall|hintUsesBulkMem|hintMutatesTable) &&
		!touchesMemory && len(modGlobals) == 0 && !hints.flags.has(hintModuleEH) &&
		len(customInstructions) == 0 && len(gcTypeLayouts) == 0 && gcFrameRoots == nil &&
		len(inlinedCallees) == 0
	if regABI {
		sc.directPrepared = directPrepared

		// Scheduler retention and register preservation are independent proofs.
		// This is the byte-backed, loop-free candidate set; module finalization
		// admits only acyclic bounded callees. Calls removed by inlining have no
		// relocation for the finalizer to charge, while bulk/table mutations may
		// lower to guest-counted native loops, so both classes fail closed here.
		// preserveCallerPins independently selects the smaller register-save thunk.
		sc.directPreparedBounded = directPrepared && len(c.BodyBytes) != 0 && !f.hasLoop &&
			!hints.flags.has(hintUsesBulkMem|hintMutatesTable) && len(inlinedCallees) == 0 &&
			len(customInstructions) == 0 && f.opt(optPreparedBoundedEntry)
		globalHints = hints.sparseGlobals
	}
	f.installModuleGlobals(modGlobals)
	f.noteResidencyEvents(hints)
	callRegions := hasCall && f.opt(optIntervalCallRegions) && !f.interruptible && !f.threadedMemory0 && len(gcTypeLayouts) == 0 && gcFrameRoots == nil &&
		!hints.flags.has(hintModuleSIMD|hintModuleEH|hintMutatesTable) && len(m.Tables) == 0 && len(customInstructions) == 0
	intervalRegion := pinLocals && regABI && (!hasCall || callRegions) && (!hints.flags.has(hintHasControlFlow) || intervalControlsEnabled && !hints.flags.has(hintModuleSIMD) && len(m.Tables) == 0 && len(customInstructions) == 0) &&
		(!hints.flags.has(hintUsesBulkMem|hintMutatesTable) || callRegions) && len(inlinedCallees) == 0 && f.prepareIntervalRegion(c.BodyBytes, hints)
	if intervalRegion && callRegions {
		f.stats.peep("interval-call-regions")
	}
	if intervalRegion {
		if !f.intervalControl {
			gpPool = nil // straight-line regional assignments supersede whole-function GP pins
		}
		if f.intervalControl {
			f.regMerge = false
		}
	}
	f.assignPinnedLocals(hints.localScore, globalHints, gpPool, hasCall, pinLocals)
	if f.intervalControl {
		f.intervalRegLimit = f.currentIntervalRegLimit()
	}

	for i := range f.locals {
		if r := f.locals[i].reg; r >= X2 && r <= X7 {
			f.stats.peep("entry-arg-local-pin")
		}
	}
	// A call-free register-ABI leaf can keep its integer parameters in the
	// incoming argument registers.  Unlike the normal X19..X23 local pins, those
	// registers are caller-clobbered, so this leaves the caller's pinned locals
	// intact across a hot direct call.  It also removes the otherwise redundant
	// internal-entry arg-to-local moves.  This is deliberately leaf-only: a
	// callee that itself makes a call must retain the normal callee-saved local
	// model for its own call boundaries.
	if f.preserveCallerPins && !f.scalarSummary.Eligible {
		f.pinLeafRegABIIntParams()
	}
	if f.pinnedLocalMask.has(mergeReg) {
		f.regMerge = false // X15 now holds a pinned local/global, so it can't be the merge register
	}
	// STACK_REG (lazy pinned-local spill) for every call-making function,
	// including memory-touching ones: dirty-only stores before a call, lazy reload
	// on the next read (WARP's model). #68 disabled this for memory functions as a
	// workaround; the actual root cause was the opElse merge edge skipping
	// reconcileLocals (fixed in control.go, TestExecIfElseLocalMerge).
	f.makesCalls = hasCall
	f.scopedConstHints.flags = hints.flags
	if f.opt(optScopedLoopIntConst) && hasCall && hints.loopIntConstCount != 0 {
		f.scopedConstHints = scopedConstHintsFrom(hints)
	}
	f.usesCalls = hasCall && policy.EnabledOption(optStackReg)
	// A call-free leaf extends the deepest checked stack by exactly one frame; the
	// fence's 256 KiB margin (runtime stackFenceMargin) absorbs that when the frame
	// is provably small. frameSize isn't known until after the body, so bound it:
	// spill slots never exceed the body's operand pushes (< one per body byte).
	f.skipFence = shouldSkipStackFence(hasCall, f.nLocalSlots, len(c.BodyBytes))
	// The return-in-register hint helps compute/call-heavy code (recursion,
	// dispatch) but adds register pressure in the deep, memory-bound call graphs
	// (json-as's TLSF/GC) where it measured as a small regression. Gate it on
	// !touchesMemory so it only fires where it's a win.
	f.singleRegResult = regABI && !touchesMemory && len(ft.Results) == 1
	if f.singleRegResult {
		rt := mtOf(ft.Results[0])
		f.resultFloat = rt.isFloat()
		f.resultF64 = rt == mtF64
	}
	f.lazyZero = hints.flags.has(hintCallsSelf) && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8

	// Auto-inlining: reserve each spliced callee's locals past f.nLocals (after all
	// nLocals-dependent setup above, so zeroDeclaredLocals/skipFence/lazyZero see the
	// caller's own locals only). Extends the frame's local arrays with unpinned
	// scratch; the splice at each call site binds/zeroes them.
	f.reserveInlineLocals(inlinedCallees, inlineTargets)
	// Reject an already-oversized locals/header frame before emitting any
	// SP-relative homes whose architecture encoding has a smaller displacement.
	// Operand spills can only grow this frame and remain checked after lowering.
	if err := f.validateFrameSize(f.frameSize()); err != nil {
		return nil, nil, 0, err
	}

	if regallocCheckEnabled {
		checkSourceBegin(f, hostAdapter || !regABI)
	}

	if regABI {
		internalOff, err := f.emitRegABI(c, hostAdapter, hints.localScore, hints.flags.has(hintHasFloatConst), hints)
		if err != nil {
			return nil, nil, 0, err
		}
		if f.gcFrameRoots != nil {
			f.gcFrameRoots.FrameBytes = uint32(f.frameSize())
			if f.gcCallsiteIndex != f.gcFrameRoots.CallMaskCount() {
				f.gcFrameRoots.Exact = false
			}
		}
		f.finalizePeepholes(internalOff)
		if err := f.emitFloatConstPool(); err != nil {
			return nil, nil, 0, err
		}
		if regallocCheckEnabled {
			checkSourceFinishEmission(f)
		}
		internalOff, err = f.finalizeNativeCode(internalOff)
		if err != nil {
			return nil, nil, 0, err
		}
		if regallocCheckEnabled {
			checkSourceVerify(f)
		}
		f.finalizeStats(len(f.a.B))
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue(hints.localScore)
	f.preloadLoopIntConsts(hints)
	if hints.flags.has(hintHasFloatConst) {
		f.preloadFloatConsts(c.BodyBytes)
	}
	f.preloadV128Consts(c.BodyBytes)
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	if profileEnabled {
		f.collectProfileSources(0)
	}
	f.emitTrapStubs()
	f.emitPhasePadding()
	if err := f.patchFrameAdjusts(); err != nil {
		return nil, nil, 0, err
	}
	if f.gcFrameRoots != nil {
		f.gcFrameRoots.FrameBytes = uint32(f.frameSize())
		if f.gcCallsiteIndex != f.gcFrameRoots.CallMaskCount() {
			f.gcFrameRoots.Exact = false
		}
	}
	f.finalizePeepholes()
	if err := f.emitFloatConstPool(); err != nil {
		return nil, nil, 0, err
	}
	if _, err := f.finalizeNativeCode(0); err != nil {
		return nil, nil, 0, err
	}
	f.finalizeStats(len(f.a.B))
	return f.a.B, f.relocs, 0, nil
}

// preservesCallerPins identifies the deliberately narrow internal-call ABI
// variant used for hot, simple leaves. Such a function has no declared locals,
// calls, memory access, or global access; its integer parameters can stay in the
// incoming argument registers while every caller-pinned register is reserved.
// Consequently it cannot observe or modify caller state outside X0..X7/X16/X17.
func preservesCallerPins(ft *wasm.CompType, nLocals int, h funcHints) bool {
	if !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || nLocals != len(ft.Params) || h.flags.has(hintHasCall|hintTouchesMemory|hintUsesBulkMem) {
		return false
	}
	if h.globalCount != 0 {
		return false
	}
	return true
}

// finalizeStats fills the per-function size counters from final compiler state
// (no-op when collection is off). Per-event counters are incremented at their
// emission sites during the body.
func (f *fn) finalizeStats(codeLen int) {
	s := f.stats
	if !diagnosticsEnabled || s == nil {
		return
	}
	s.CodeBytes = codeLen
	s.NativeSize.TotalBytes = codeLen
	s.NativeSize.InternalFunctionBytes = codeLen - s.NativeSize.HostAdapterBytes - s.NativeSize.AdapterToInternalPaddingBytes
	s.GCCodeBytes.Total = codeLen
	s.peepN("logical-move-immediate", f.a.LogicalMoveImmediates)
	s.peepN("compact-move-immediate32", f.a.CompactMoveImmediates32)
	s.peepN("single-negative-move32", f.a.SingleNegativeMoves32)
	s.peepN("indexed-base-reuse", f.a.IndexedBaseReuses)
	s.FrameBytes = f.frameSize()
	s.MaxSpillSlots = f.maxSpill
	s.MaxPendingNodes = int(f.s.maxPending)
}

// runBody opens the function control frame, lowers the body, and patches every
// return/br-to-function site to the current epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	f.innermostLoop = 0
	if f.scalarSummary.Eligible {
		f.ctrl = f.sc.ctrl[:0]
		return f.scalarBody(c)
	}

	sc := f.scratchState()
	resultTypes := lowerFunctionResultTypes(sc, f.ft.Results)
	if len(resultTypes) <= len(sc.functionResultTypeArena) {
		f.controlBaseTypeN = uint8(len(resultTypes))
	}
	f.ctrl = append(sc.ctrl[:0], ctrlFrame{kind: cfFunc, resultN: len(resultTypes), branchN: uint32(len(resultTypes)), types: resultTypes})
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	f.patchReturnSites()
	return nil
}

func (f *fn) appendReturnSite(site int) {
	if site < 0 || site&3 != 0 || site/4 >= 1<<26-1 {
		f.setRepresentationLimit(functionRepresentationReturnSite)
		return
	}
	sc := f.scratchState()
	word := rdWord(f.a.B, site)
	wrWord(f.a.B, site, word&0xfc000000|sc.retSiteHead)
	sc.retSiteHead = uint32(site/4 + 1)
}

func (f *fn) patchReturnSites() {
	sc := f.scratchState()
	for head := sc.retSiteHead; head != 0; {
		site := int(head-1) * 4
		word := rdWord(f.a.B, site)
		head = word & 0x03ffffff
		wrWord(f.a.B, site, word&0xfc000000)
		f.patchBranch26(site, f.a.Len())
	}
}

func gpPinPoolWithPolicy(pool []Reg, regABI bool, nParams int, callFree bool, policy CodegenPolicy) []Reg {
	pool = append(pool, pinnedLocalRegs...) // X19-X23
	pool = append(pool, X24, X25)
	if callFree && policy.EnabledOption(optX8Pin) {
		pool = append(pool, X8)
		// X8 is neither an internal integer argument (X0-X7) nor a fixed-role
		// backend scratch.
	}
	if !regABI || nParams <= 4 {
		pool = append(pool, X9, X10, X11)
	}
	pool = append(pool, X15)
	if !callFree {
		pool = append(pool, X27)
	}
	return pool
}

func callSafePinPool(pool []Reg) []Reg {
	safe := pool[:0]
	for _, reg := range pool {
		if reg >= X19 && reg <= X25 {
			safe = append(safe, reg)
		}
	}
	return safe
}

// gpPinLimit leaves enough of the target's unreserved allocatable file for the
// widest ordinary lowering step: three protected inputs/temporaries plus one
// result. Whole-function pins are optional; transient lowering must not retry a
// function merely because module roles made the physical file smaller.
func gpPinLimit(reserved regMask) int {
	const minTransientGP = 4
	available := 0
	for _, r := range gpAlloc {
		if !reserved.has(r) {
			available++
		}
	}
	if available <= minTransientGP {
		return 0
	}
	return available - minTransientGP
}

// withoutReg returns pool with r removed (order preserved).
func withoutReg(pool []Reg, r Reg) []Reg {
	out := pool[:0]
	for _, p := range pool {
		if p != r {
			out = append(out, p)
		}
	}
	return out
}

func (f *fn) assignPinnedLocals(scores []uint32, globalHints []shared.GlobalHint, gpPool []Reg, hasCall, pinLocals bool) {
	if cap(f.locals) < f.nLocals {
		f.locals = make([]localDef, f.nLocals)
	} else {
		f.locals = f.locals[:f.nLocals]
	}
	for i := range f.locals {
		facts := valueFacts(0)
		if f.localFactsEnabled && i >= f.nParams && f.localType[i] == mtI32 {
			facts = factUpper32Zero | factBoolean // declared locals start at zero
		}
		f.locals[i] = localDef{reg: regNone, facts: facts, state: lsReg}
	}
	// Module-pinned globals (installModuleGlobals) already occupy globalReg
	// entries; keep them and size for whichever view is larger.
	if len(f.globalReg) < f.m.GlobalCount() {
		f.initGlobalRegs(f.m.GlobalCount())
	}
	// The GP pin pool is shared by hot INT locals and hot globals, both holding their
	// VALUE in the register (WARP's model). A global is a candidate only when it is a
	// mutable int accessed inside a loop (score >= one loop level): WARP pins only int
	// globals as values, and the loop gate ensures the per-iteration memory traffic it
	// removes outweighs the one-time prologue load + epilogue write-back.
	var gpStorage [32]gpCand // no target can dedicate more than its physical register file
	if len(gpPool) > len(gpStorage) {
		panic("arm64: GP pin pool exceeds architectural register file")
	}
	gp := gpStorage[:0]
	if len(gpPool) != 0 {
		if f.nLocals > 64 {
			for i := 0; i < f.nLocals && i < 256; i++ {
				if (f.localType[i] == mtI32 || f.localType[i] == mtI64) && i < len(f.wideReuseScore) && f.wideReuseScore[i] != 0 {
					gp = insertGPCandidate(gp, gpCand{idx: uint32(i), score: f.wideReuseScore[i]}, len(gpPool))
				}
			}
		} else {
			for i := 0; i < f.nLocals; i++ {
				if f.localType[i] == mtI32 || f.localType[i] == mtI64 {
					gp = insertGPCandidate(gp, gpCand{idx: uint32(i), score: localHotness(scores[i])}, len(gpPool))
				}
			}
		}
	}
	loopMin := uint32(loopWeight(1))
	// Wide-table reservations are limited to the analyzed locals.
	for _, gh := range globalHints {
		if f.nLocals > 64 {
			continue
		}
		g := int(gh.Index)
		if gh.Score < loopMin || f.isModuleGlobal(g) || hasCall && !gh.Eligible {
			continue
		}
		gt, ok := f.globalType(gh.Index)
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		gp = insertGPCandidate(gp, gpCand{global: true, idx: uint32(g), score: gh.Score}, len(gpPool))
	}
	for k, c := range gp {
		if k >= len(gpPool) {
			break
		}
		// The extended pool slots (beyond the X19-X23 base) only take locals that
		// are actually used (score > 0): pinning a cold local there costs prologue
		// and call-spill traffic for nothing. Zero-score candidates still fill the
		// base slots so byte-backed decoded bodies keep the first-N fallback.
		if (k >= len(pinnedLocalRegs) || f.nLocals > 64) && c.score == 0 {
			break
		}
		// X27 is the last, opportunistic call-making pin. Spending it adds one
		// more entry home and call-boundary state transition, so require enough
		// loop-weighted traffic to repay that fixed cost. Twenty loop-level local
		// references (20*loopWeight(1)) separates dense kernels from marginal
		// residents without changing the established X19-X25/X15 assignments.
		if gpPool[k] == X27 && hasCall && c.score < 200 {
			break
		}
		if gpPool[k] == X27 {
			f.stats.peep("cold-call-x27-pin")
		}
		idx := int(c.idx)
		if c.global {
			f.globalReg[idx] = gpPool[k]
			f.stats.addPinnedGlobalValue()
		} else {
			f.locals[idx].reg = gpPool[k]
			f.stats.addPinnedLocal()
			if f.nLocals > 64 {
				f.stats.peep("wide-reuse-local-pin")
				if diagnosticsEnabled && f.stats != nil {
					f.stats.peep(fmt.Sprintf("wide-reuse-local-%d", idx))
				}
			}
		}
		f.pinnedLocalMask = f.pinnedLocalMask.add(gpPool[k])
	}
	// Float locals use the separate V pin pool. Call-free functions also pin hot
	// v128 locals here (same V registers, full 128-bit): a wasm→wasm call would only
	// preserve the low 64 bits, so a v128 pin is confined to the call-free class.
	pinV128 := f.opt(optV128Pins) && !hasCall
	hotV128Candidates := 0
	if pinLocals && f.nLocals <= 64 {
		for i := 0; i < f.nLocals; i++ {
			if pinV128 && f.localType[i] == mtV128 && localHotness(scores[i]) > 0 {
				hotV128Candidates++
			}
		}
	}
	fpPinLimit := len(pinnedFLocalRegs)
	deepV128Pins := false
	if !f.opt(optExtendedFPPins) && fpPinLimit > basePinnedFLocalRegs {
		fpPinLimit = basePinnedFLocalRegs
	} else if !hasCall && hotV128Candidates >= 24 {
		// Wide vector kernels have short expression trees but large named v128
		// state. Keeping that state resident is worth the smaller transient pool;
		// scalar-float kernels retain the measured 15-pin cap below.
		fpPinLimit = len(pinnedFLocalRegs)
		deepV128Pins = true
		f.stats.peep("deep-v128-local-pins")
	} else if !hasCall && fpPinLimit > callFreePinnedFLocalRegs {
		// Call-free numeric loops still need room for wide expression trees. Past
		// this point nbody loses more to transient-register pressure than it gains
		// from another local pin. Call-making raytrace has sparse call sites and a
		// much larger live-local set, so its existing STACK_REG path profitably uses
		// the full pool.
		fpPinLimit = callFreePinnedFLocalRegs
	} else if hasCall && fpPinLimit > 27 {
		fpPinLimit = 27
	}
	if !pinLocals || f.nLocals > 64 {
		// Keep very wide signatures canonical so optional V-register pins cannot
		// consume the transient register floor.
		fpPinLimit = 0
	}
	var fcStorage [32]uint16 // bounded by the 32-register architectural V file
	if fpPinLimit > len(fcStorage) {
		panic("arm64: FP pin limit exceeds architectural register file")
	}
	fc := fcStorage[:0]
	if fpPinLimit != 0 {
		for i := 0; i < f.nLocals; i++ {
			if f.localType[i].isFloat() || (pinV128 && f.localType[i] == mtV128) {
				fc = insertLocalCandidate(fc, uint16(i), scores, fpPinLimit)
			}
		}
	}
	usedFP := 0
	for k, local := range fc {
		if k >= fpPinLimit {
			break
		}
		i := int(local)
		if deepV128Pins && localHotness(scores[i]) == 0 {
			break
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
		f.stats.addPinnedLocal()
		usedFP++
	}
	f.assignPinnedFloatGlobals(globalHints, hasCall, loopMin, usedFP, fpPinLimit)
}

// pinLeafRegABIIntParams maps integer parameters of a call-free register-ABI
// function onto X0..X7, their incoming locations at the internal entry.  The
// normal local allocator may already have selected a callee-saved pin for the
// parameter; release that pin before installing the argument register.
func (f *fn) pinLeafRegABIIntParams() {
	gp := 0
	for i := 0; i < f.nParams; i++ {
		if f.localType[i].isFloat() {
			continue
		}
		if gp >= len(intArgRegs) {
			return
		}
		if old := f.locals[i].reg; old != regNone {
			f.pinnedLocalMask = f.pinnedLocalMask.remove(old)
		}
		f.locals[i].reg = intArgRegs[gp]
		f.pinnedLocalMask = f.pinnedLocalMask.add(intArgRegs[gp])
		gp++
	}
}

// derivePinnedGlobals loads each pinned global's cell pointer into its dedicated
// register, once, in the prologue (linMemReg = linMem must already be set). A no-op when
// no globals are pinned. Every later access reads/writes through the register.
func (f *fn) globalIs64(g int) bool {
	gt, _ := f.globalType(uint32(g))
	return wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64)
}

func globalRegIsFloat(state Reg) bool { return state != regNone && state&globalRegFloat != 0 }

func globalRegValue(state Reg) Reg {
	if state == regNone {
		return regNone
	}
	return state &^ (globalRegDirty | globalRegFloat)
}

func globalRegIsDirty(state Reg) bool {
	return state != regNone && state&globalRegDirty != 0
}

func (f *fn) initGlobalRegs(n int) {
	if cap(f.globalReg) < n {
		f.globalReg = make([]Reg, n)
	} else {
		f.globalReg = f.globalReg[:n]
	}
	for i := range f.globalReg {
		f.globalReg[i] = regNone
	}
}

// installModuleGlobals records the module-wide global→register pins on this
// function (every function in the module shares the same assignment).
func (f *fn) installModuleGlobals(pins []moduleGlobalPin) {
	if len(pins) == 0 {
		return
	}
	nG := f.m.GlobalCount()
	if len(f.globalReg) < nG {
		f.initGlobalRegs(nG)
	}
	f.moduleGlobals = pins
	for _, p := range pins {
		f.globalReg[p.global] = p.reg
	}
}

func (f *fn) isModuleGlobal(g int) bool {
	for _, p := range f.moduleGlobals {
		if int(p.global) == g {
			return true
		}
	}
	return false
}

// deriveModuleGlobals / storeModuleGlobals sync the module-pinned globals with
// their cells at wasm↔native boundaries (offset-0 prologues and epilogues, the
// adapter's Go exit, trap stubs) and before wrapper-ABI calls (whose callee's
// offset-0 prologue reloads). Register-ABI calls and returns carry nothing.
// scratch must be a register safe to clobber at the call site.
func (f *fn) deriveModuleGlobals() {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || !f.isModuleGlobal(g) {
			continue
		}
		f.ld64(reg, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(reg, reg, int32(g*8))
		if f.globalIs64(g) {
			f.ld64(reg, reg, 0)
		} else {
			f.ld32(reg, reg, 0)
		}
	}
}

func (f *fn) storeModuleGlobals(scratch Reg) {
	f.storeGlobalPins(scratch, false)
}

// Trap exits also persist dirty function-local value pins using a fixed scratch.
func (f *fn) storeGlobalPins(scratch Reg, valuePins bool) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || (!f.isModuleGlobal(g) && (!valuePins || !globalRegIsDirty(state))) {
			continue
		}
		f.ld64(scratch, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(scratch, scratch, int32(g*8))
		f.storePinnedGlobalValue(g, state, scratch)
	}
}

// derivePinnedGlobals loads each value-pinned global's current value into its
// register from memory (base → &cell → value, reusing the register for the chain).
// Used in the prologue and to reload after a call (the callee may have changed the
// shared global). A no-op when no globals are pinned.
func (f *fn) derivePinnedGlobals() {
	f.derivePinnedGlobalsIn(^regMask(0))
}

// derivePinnedGlobalsIn reloads only globals assigned to registers in regs.
// Fixed-register sequences use it to restore their narrow scratch bank without
// touching unrelated value pins.
func (f *fn) derivePinnedGlobalsIn(regs regMask) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || f.isModuleGlobal(g) {
			continue
		}
		if globalRegIsFloat(state) {
			// Narrow masks describe GP clobbers, not the overlapping V-register bank.
			if regs != ^regMask(0) {
				continue
			}
			cell := f.allocReg(maskOf(X0))
			f.ld64(cell, linMemReg, -int32(abi.GlobalsPtrOffset))
			f.ld64(cell, cell, int32(g*8))
			gt, _ := f.globalType(uint32(g))
			f.a.LdrF(reg, cell, 0, wasm.EqualValType(wasm.GlobalValueType(gt), wasm.F64))
			f.release(cell)
		} else {
			if !regs.has(reg) {
				continue
			}
			f.ld64(reg, linMemReg, -int32(abi.GlobalsPtrOffset))
			f.ld64(reg, reg, int32(g*8))
			if f.globalIs64(g) {
				f.ld64(reg, reg, 0)
			} else {
				f.ld32(reg, reg, 0)
			}
		}
	}
}

// storePinnedGlobals writes value-pinned globals' registers back to their memory
// cells. dirtyOnly (epilogue) writes only the globals this function actually set;
// the call path (dirtyOnly=false) writes all of them before a call so the callee
// observes the current value. Avoids X0 (the int result register) for the
// cell-address scratch.
func (f *fn) storePinnedGlobals(dirtyOnly bool) {
	f.storePinnedGlobalsIn(^regMask(0), dirtyOnly)
}

// storePinnedGlobalsIn writes only globals assigned to registers in regs.
func (f *fn) storePinnedGlobalsIn(regs regMask, dirtyOnly bool) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || f.isModuleGlobal(g) || (globalRegIsFloat(state) && regs != ^regMask(0)) || (!globalRegIsFloat(state) && !regs.has(reg)) || (dirtyOnly && !globalRegIsDirty(state)) {
			continue
		}
		t := f.allocReg(maskOf(reg, X0))
		f.ld64(t, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(t, t, int32(g*8))
		f.storePinnedGlobalValue(g, state, t)
		f.release(t)
	}
}

// prologue: frameless body — pin linMem in linMemReg (moved from X1 per WARP's
// convention), save FP/LR in call-making functions, reserve the frame with one
// `SUB SP,SP,#frameSize`, stash the results ptr in the SP-relative header, load
// params into their register or slot, zero declared locals.
func (f *fn) prologue(localScores []uint32) {
	a := f.a
	if f.makesCalls {
		a.StpPre(FP, LR, SP, -16) // save FP/LR frame record (BL clobbers LR)
		a.AddImm64(FP, SP, 0)     // MOV X29, SP — frame pointer for backtraces
	}
	// Frame reserve: a fixed MOVZ/MOVK X16 + `SUB SP,SP,X16` sequence whose two mov
	// immediates are patched with frameSize after the body (the SUB-imm form is only
	// 12 bits, so we materialize the size in the backend scratch X16 — uniform for
	// any frame size). See CONTRACT §4h option 1.
	f.subRspAt = a.Len()
	a.Movz64(X16, 0, 0)       // frame size lo 16 bits; patched after body
	a.Movk64(X16, 0, 1)       // frame size hi 16 bits
	a.SubSPReg(X16)           // SUB SP, SP, X16
	a.MovReg64(linMemReg, X1) // linMem → linMemReg (pinned for the whole function)

	f.st64(SP, frResultsOff, X3) // results ptr (trap cell ptr lives in basedata)
	if f.memSizeReg != regNone {
		// Offset-0 entry: establish the module-wide memBytes cache. Direct wasm→wasm
		// register-ABI calls skip this (the caller's value is valid by construction).
		f.loadMemoryCache()
		if f.memLimitReg != regNone {
			f.a.SubImm64(f.memLimitReg, f.memSizeReg, uint32(f.memLimitExtent))
		}
	}
	f.emitStackFenceCheck(linMemReg, X16)
	f.emitInterruptCheck(false)
	f.entryTrapEnd = a.Len()
	if f.cacheImportDispatch {
		f.ld64(X25, linMemReg, -int32(offImportDispatchPtr))
	}
	if f.cacheNativeImportTarget {
		f.ld64(X24, X25, coreruntime.ImportDispatchCodePtrOffset)
		f.a.AddImm64(X24, X24, coreruntime.NativeScalarLeafRegisterEntryOffset)
	}
	// Copy v128 params through V0 before loading any pinned scalar float params.
	// V0 is only a prologue scratch here; keeping these copies first prevents a
	// future pin-pool change from letting a later v128 copy clobber an already-live
	// scalar param register. X0 is the serArgs base (wrapper-ABI arg 0).
	paramOff := int32(0)
	for i, pt := range f.ft.Params {
		if f.localType[i] == mtV128 {
			if pr, _, ok := f.pinReg(i); ok {
				a.VMovdquLoadDisp(pr, X0, paramOff) // pinned v128 param → its V register
			} else {
				a.VMovdquLoadDisp(0, X0, paramOff)
				a.VMovdquStoreDisp(SP, f.localOff(i), 0)
			}
		}
		paramOff += abiValSize(pt)
	}
	x0ParamOff := int32(-1) // a param pinned in X0 must load LAST: X0 is the args base
	x0ParamI32 := false
	pairParams := f.opt(optEntryParamPairs)
	pendingParam := pendingWrapperParamHome{}
	paramOff = 0
	for i, pt := range f.ft.Params {
		if f.localType[i] != mtV128 {
			if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				if pr == X0 {
					x0ParamOff = paramOff
					x0ParamI32 = f.canonicalI32Local(i)
				} else if f.canonicalI32Local(i) {
					f.ld32(pr, X0, paramOff)
					f.stats.peep("entry-i32-param-canonicalize")
				} else {
					f.ld64(pr, X0, paramOff) // pinned int param → its GP register
				}
			} else if ok && isFloat {
				a.FLoadDisp(pr, X0, paramOff, f.localType[i] == mtF64) // pinned float param → V reg
			} else if f.canonicalI32Local(i) {
				if pairParams {
					f.flushWrapperParamHome(pendingParam)
					pendingParam = pendingWrapperParamHome{}
				}
				f.ld32(X16, X0, paramOff)
				f.st64(SP, f.localOff(i), X16)
				f.stats.peep("entry-i32-param-canonicalize")
			} else {
				if pairParams {
					pendingParam = f.queueWrapperParamHome(pendingParam, paramOff, f.localOff(i))
				} else {
					// X16 (backend scratch) is the copy temp: X0 is the serArgs base and
					// must stay live for the remaining param loads (amd64 used RAX here,
					// but on arm64 that role register aliases the args base).
					f.ld64(X16, X0, paramOff)
					f.st64(SP, f.localOff(i), X16)
				}
			}
		}
		paramOff += abiValSize(pt)
	}
	if pairParams {
		f.flushWrapperParamHome(pendingParam)
	}
	if x0ParamOff >= 0 {
		if x0ParamI32 {
			f.ld32(X0, X0, x0ParamOff)
			f.stats.peep("entry-i32-param-canonicalize")
		} else {
			f.ld64(X0, X0, x0ParamOff)
		}
	}
	f.zeroDeclaredLocals(localScores)
	f.derivePinnedGlobals()
	f.deriveModuleGlobals() // offset-0 entry: cells → module-pinned registers
}

func (f *fn) queueWrapperParamHome(p pendingWrapperParamHome, argOff, localOff int32) pendingWrapperParamHome {
	if p.valid && argOff == p.argOff+8 && localOff == p.localOff+8 && p.argOff <= 504 && p.localOff <= 504 {
		f.a.LdpOffset(X16, X17, X0, p.argOff)
		f.a.StpOffset(X16, X17, SP, p.localOff)
		f.stats.peep("entry-param-pair-wrapper")
		return pendingWrapperParamHome{}
	}
	if p.valid {
		f.ld64(X16, X0, p.argOff)
		f.st64(SP, p.localOff, X16)
	}
	return pendingWrapperParamHome{argOff: argOff, localOff: localOff, valid: true}
}

func (f *fn) flushWrapperParamHome(p pendingWrapperParamHome) {
	if p.valid {
		f.ld64(X16, X0, p.argOff)
		f.st64(SP, p.localOff, X16)
	}
}

// zeroDeclaredLocals initializes non-parameter locals. Most functions keep the
// old eager zeroing path; small call+memory functions use WARP-style lazy zero,
// where reads materialize zero on demand and control-flow reconciliation stores it
// to the frame before paths diverge when required.
func (f *fn) zeroDeclaredLocals(localScores []uint32) {
	if f.scalarSummary.Eligible {
		// ScalarState owns zero values and establishes homes only when needed.
		return
	}
	f.zeroEHGCRootLanes()
	if f.nLocals <= f.nParams {
		return
	}
	if !f.lazyZero {
		a := f.a
		pairZeros := f.opt(optEntryZeroPairs)
		entryInitElision := f.opt(optEntryInitElision) && (f.gcFrameRoots == nil || !f.gcFrameRoots.Candidate)
		pendingZero := int32(-1)
		// AArch64 has a zero register (XZR): store it directly, no scratch to clear.
		for i := f.nParams; i < f.nLocals; i++ {
			if entryInitElision && i < 64 && localScores[i]&localScoreEntryInitialized != 0 {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				f.stats.peep("entry-init-elide")
				continue
			}
			if pr, _, ok := f.pinReg(i); ok && f.localType[i] == mtV128 {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.NeonEor16b(pr, pr, pr) // zero the whole 128-bit pin register
			} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.MovImm64(pr, 0)
			} else if ok && isFloat {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.FmovFromGpr(pr, ZR, false) // fmov d,xzr → 0.0
			} else if f.localType[i] == mtV128 {
				if pairZeros {
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i))
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i)+8)
				} else {
					f.st64(SP, f.localOff(i), ZR)
					f.st64(SP, f.localOff(i)+8, ZR)
				}
			} else {
				if pairZeros {
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i))
				} else {
					f.st64(SP, f.localOff(i), ZR)
				}
			}
		}
		if pairZeros {
			f.flushDeclaredZeroSlot(pendingZero)
		}
		return
	}
	for i := f.nParams; i < f.nLocals; i++ {
		f.markDeclaredLocalZero(i)
	}
}

func (f *fn) queueDeclaredZeroSlot(pending, off int32) int32 {
	if pending >= 0 && off == pending+8 && pending <= 504 {
		f.a.StpOffset(ZR, ZR, SP, pending)
		f.stats.peep("entry-zero-pair")
		return -1
	}
	if pending >= 0 {
		f.st64(SP, pending, ZR)
	}
	return off
}

func (f *fn) flushDeclaredZeroSlot(pending int32) int32 {
	if pending >= 0 {
		f.st64(SP, pending, ZR)
	}
	return -1
}

// emitStackFenceCheck traps (StackFence → "call stack exhausted") when SP has
// dropped below the fence stored at [linMem-72], turning unbounded recursion into
// a clean trap instead of a fault. A zero fence disables the check (SP > 0).
func (f *fn) emitStackFenceCheck(linMemReg, scratch Reg) {
	if !f.opt(optStackFence) || f.skipFence {
		return
	}
	f.ld64(scratch, linMemReg, -72)
	f.a.CmpSP64(scratch)            // CMP SP, scratch (SP-in-Rn extended-register form)
	f.trapIf(condB, trapStackFence) // SP below the fence → cold stub
}

// emitHostAdapter emits the wrapper-to-register ABI bridge and returns the BL
// site that will be patched to the internal entry after the function body.
func (f *fn) emitHostAdapter(np, rN int) int {
	a := f.a
	a.MovReg64(linMemReg, X1) // linMem → linMemReg: the module-wide invariant the internal entry inherits
	if f.memSizeReg != regNone {
		// Offset-0 entry (from Go, or an indirect call): establish the module-wide
		// memBytes cache before the internal entry runs (which relies on it).
		f.loadMemoryCache()
	}
	f.deriveModuleGlobals()   // offset-0 entry: cells → module-pinned registers
	a.StpPre(LR, X3, SP, -16) // save LR (BL clobbers it) + results ptr; keeps SP 16-aligned
	gp, fp := 0, 0
	x0ArgOff := int32(-1) // the arg targeting X0 aliases the serArgs base: load it LAST
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			a.FLoadDisp(fpArgRegs[fp], X0, int32(8*i), mt == mtF64)
			fp++
		} else {
			if intArgRegs[gp] == X0 {
				x0ArgOff = int32(8 * i)
			} else {
				f.ld64(intArgRegs[gp], X0, int32(8*i))
			}
			gp++
		}
	}
	if x0ArgOff >= 0 {
		f.ld64(X0, X0, x0ArgOff)
	}
	adapterCall := a.Bl()
	f.adapterReturnOff = adapterCall + 4
	if f.gcFrameRoots != nil {
		f.gcFrameRoots.AdapterReturnOffset = uint32(adapterCall + 4)
	}
	if registerQuadResultsSupported && rN > 2 && !sigIsFloatOnly(f.ft) {
		// X3 may be result 3; restore the results pointer into X8 instead.
		a.LdpPost(LR, X8, SP, 16)
		gp, fp := 0, 0
		for i, typ := range f.ft.Results {
			if mtOf(typ).isFloat() {
				a.FStoreDisp(X8, int32(i*8), Reg(fp), mtOf(typ) == mtF64)
				fp++
			} else {
				f.st64(X8, int32(i*8), []Reg{X0, X1, X2, X3, X4, X5, X6, X7}[gp])
				gp++
			}
		}
	} else {
		a.LdpPost(LR, X3, SP, 16) // restore LR + results ptr
	}
	f.storeModuleGlobals(X2) // Go exit: module-pinned registers → cells
	if preparedDirectFloatSupported && rN > 2 && sigIsFloatOnly(f.ft) {
		for i, typ := range f.ft.Results {
			a.FStoreDisp(X3, int32(i*8), Reg(i), mtOf(typ) == mtF64)
		}
	}
	if rN == 1 {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FStoreDisp(X3, 0, 0, rt == mtF64)
		} else {
			f.st64(X3, 0, X0)
		}
	} else if rN == 2 {
		if preparedDirectFloatSupported && mtOf(f.ft.Results[0]).isFloat() && !mtOf(f.ft.Results[1]).isFloat() {
			a.FStoreDisp(X3, 0, 0, mtOf(f.ft.Results[0]) == mtF64)
			f.st64(X3, 8, X0)
		} else if preparedDirectFloatSupported && !mtOf(f.ft.Results[0]).isFloat() && mtOf(f.ft.Results[1]).isFloat() {
			f.st64(X3, 0, X0)
			a.FStoreDisp(X3, 8, 0, mtOf(f.ft.Results[1]) == mtF64)
		} else if preparedDirectFloatSupported && mtOf(f.ft.Results[0]).isFloat() {
			for i, typ := range f.ft.Results {
				a.FStoreDisp(X3, int32(i*8), Reg(i), mtOf(typ) == mtF64)
			}
		} else {
			f.st64(X3, 0, X0)
			f.st64(X3, 8, X1)
		}
	}
	a.Ret()
	f.adapterEndOff = a.Len()
	if diagnosticsEnabled && f.stats != nil {
		f.stats.NativeSize.HostAdapterBytes = a.Len()
	}
	return adapterCall
}

// emitRegABI emits a register-ABI function as [host adapter | internal entry].
// The adapter at offset 0 keeps the wrapper ABI working for exports/host calls;
// the internal entry takes args in GP/V registers and returns numeric results
// in independent GP/FP banks.
// Returns the internal entry's offset within the function's code.
func (f *fn) emitRegABI(c *wasm.Func, hostAdapter bool, localScores []uint32, hasFloatConst bool, intConstHints *funcHintView) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in X0=serArgs, X1=linMem, X2=trap,
	// X3=results; loads args into registers, calls the internal entry, stores the
	// register results.
	gp, fp := 0, 0
	var adapterCall int
	cachedAdapter := false
	if hostAdapter {
		if f.sc != nil {
			if template, returnOff, endOff, ok := f.sc.adapterTemplate.lookup(f.ft, f.memSizeReg); ok {
				a.B = append(a.B, template...)
				f.adapterReturnOff = returnOff
				f.adapterEndOff = endOff
				adapterCall = returnOff - 4
				cachedAdapter = true
				if f.gcFrameRoots != nil {
					f.gcFrameRoots.AdapterReturnOffset = uint32(returnOff)
				}
				if diagnosticsEnabled && f.stats != nil {
					f.stats.NativeSize.HostAdapterBytes = endOff
					f.stats.NativeSize.AdapterToInternalPaddingBytes = len(template) - endOff
				}
			}
		}
		if !cachedAdapter {
			adapterCall = f.emitHostAdapter(np, rN)
		}
	}

	// Internal entry (frameless): linMemReg (linMem) is inherited from the caller —
	// every wasm function keeps it pinned, and the adapter establishes it at the
	// Go boundary — and the trap cell pointer lives in basedata, so the entry
	// carries no environment setup at all (WARP's model). Args in GP/V regs.
	if hostAdapter && !cachedAdapter {
		beforeAlign := a.Len()
		if f.adapterBacklink {
			// A 4-byte backlink directly below the internal entry holds its
			// distance from this function's offset-0 adapter. An internal funcref
			// descriptor names only the internal entry; a wrapper-ABI caller
			// tail-enters such a target through the adapter (see
			// emitTailWrapperToAdapterJump). The adapter ends in RET, so the word
			// is never executed, and cached adapter templates carry it along.
			log2 := f.policy.InternalAlignLog2
			if log2 == 0 {
				log2 = 4
			}
			for range alignmentPadding(a.Len()+4, log2) / 4 {
				a.Nop()
			}
			a.B = binary.LittleEndian.AppendUint32(a.B, uint32(a.Len()+4))
		} else {
			f.alignCode(f.policy.InternalAlignLog2)
		}
		if diagnosticsEnabled && f.stats != nil {
			f.stats.NativeSize.AdapterToInternalPaddingBytes = a.Len() - beforeAlign
		}
	}
	internalOff := a.Len()
	f.beginNativeImportVariant()
	if f.makesCalls {
		a.StpPre(FP, LR, SP, -16) // save FP/LR frame record (BL clobbers LR)
		a.AddImm64(FP, SP, 0)     // MOV X29, SP
	}
	f.subRspAt = a.Len()
	a.Movz64(X16, 0, 0)
	a.Movk64(X16, 0, 1)
	a.SubSPReg(X16)
	// X16 (backend scratch) is the fence scratch: the reg-ABI args occupy X0-X7 at
	// entry, so an arg register cannot double as scratch here (amd64 used RSI, which
	// is not one of its arg registers).
	f.emitStackFenceCheck(linMemReg, X16)

	f.emitInterruptCheck(false)
	f.entryTrapEnd = a.Len()
	if f.cacheImportDispatch {
		f.ld64(X25, linMemReg, -int32(offImportDispatchPtr))
	}
	if f.cacheNativeImportTarget {
		f.ld64(X24, X25, coreruntime.ImportDispatchCodePtrOffset)
		f.a.AddImm64(X24, X24, coreruntime.NativeScalarLeafRegisterEntryOffset)
	}
	gp, fp = 0, 0
	moves := f.tmpMoves[:0]
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			src := fpArgRegs[fp]
			if pr, isFloat, ok := f.pinReg(i); ok && isFloat {
				a.FMov(pr, src, mt == mtF64)
			} else {
				a.FStoreDisp(SP, f.localOff(i), src, mt == mtF64)
			}
			fp++
		} else if f.scalarSummary.Eligible {
			// Transfer the untouched incoming register to shared ownership. There
			// is no valid frame copy until the shared state explicitly stores one.
			if f.canonicalI32Local(i) {
				a.MovReg32(intArgRegs[gp], intArgRegs[gp])
				f.stats.peep("entry-i32-param-canonicalize")
			}
		} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
			if f.canonicalI32Local(i) {
				a.MovReg32(intArgRegs[gp], intArgRegs[gp])
				f.stats.peep("entry-i32-param-canonicalize")
			}
			if pr != intArgRegs[gp] {
				moves = append(moves, regMove{dst: pr, src: intArgRegs[gp]})
			}
		} else {
			if f.canonicalI32Local(i) {
				a.MovReg32(intArgRegs[gp], intArgRegs[gp])
				f.stats.peep("entry-i32-param-canonicalize")
			}
			f.st64(SP, f.localOff(i), intArgRegs[gp])
		}
		if !mt.isFloat() {
			gp++
		}
	}
	var checkMoves func()
	if regallocCheckEnabled {
		checkMoves = f.checkBeginRegMoves(moves, false)
		defer checkMoves()
	}
	swapChains := f.emitRegMovesWindow(moves, false)
	if regallocCheckEnabled {
		checkMoves()
	}
	f.stats.peepN("machine-swap-chain", swapChains)
	f.tmpMoves = moves[:0]
	f.zeroDeclaredLocals(localScores)
	if hasFloatConst {
		f.preloadFloatConsts(c.BodyBytes)
	}
	f.preloadV128Consts(c.BodyBytes)
	f.derivePinnedGlobals()
	f.preloadLoopIntConsts(intConstHints)
	if f.memSizeReg == X17 {
		// X17 is caller-clobbered backend scratch, so establish this leaf-local
		// cache after entry setup and immediately before the body that consumes it.
		f.ld64(X17, linMemReg, -bdCurBytes)
	}
	if f.memLimitReg != regNone {
		// The register-ABI internal entry can be reached directly from another Wasm
		// function, so derive the function-local adjusted limit here rather than in
		// the host adapter alone.
		f.a.SubImm64(f.memLimitReg, f.memSizeReg, uint32(f.memLimitExtent))
	}
	if err := f.runBody(c); err != nil {
		return 0, err
	}
	// Body results are canonical; terminal reloads may reuse cache registers.
	// Attempt cleanup owns observer restoration if return emission panics.
	var returnWrites regallocWriteMask
	if regallocCheckEnabled {
		returnWrites = f.checkTerminalWrites()
	}
	f.storePinnedGlobals(true) // write dirty value-pinned globals back to their cells (all returns land here)
	if rN == 1 && !f.singleRegResult {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FLoadDisp(0, SP, f.spillOff(0), rt == mtF64) // result -> V0
		} else {
			f.ld64(X0, SP, f.spillOff(0)) // result -> X0
		}
	} else if rN == 2 {
		if preparedDirectFloatSupported && mtOf(f.ft.Results[0]).isFloat() && !mtOf(f.ft.Results[1]).isFloat() {
			a.FLoadDisp(0, SP, f.spillOff(0), mtOf(f.ft.Results[0]) == mtF64)
			f.ld64(X0, SP, f.spillOff(1))
		} else if preparedDirectFloatSupported && !mtOf(f.ft.Results[0]).isFloat() && mtOf(f.ft.Results[1]).isFloat() {
			f.ld64(X0, SP, f.spillOff(0))
			a.FLoadDisp(0, SP, f.spillOff(1), mtOf(f.ft.Results[1]) == mtF64)
		} else if preparedDirectFloatSupported && mtOf(f.ft.Results[0]).isFloat() {
			for i, typ := range f.ft.Results {
				a.FLoadDisp(Reg(i), SP, f.spillOff(i), mtOf(typ) == mtF64)
			}
		} else {
			f.ld64(X0, SP, f.spillOff(0))
			f.ld64(X1, SP, f.spillOff(1))
		}
	}
	if preparedDirectFloatSupported && rN > 2 && sigIsFloatOnly(f.ft) {
		for i, typ := range f.ft.Results {
			a.FLoadDisp(Reg(i), SP, f.spillOff(i), mtOf(typ) == mtF64)
		}
	} else if registerQuadResultsSupported && rN > 2 {
		gp, fp := 0, 0
		for i, typ := range f.ft.Results {
			if mtOf(typ).isFloat() {
				a.FLoadDisp(Reg(fp), SP, f.spillOff(i), mtOf(typ) == mtF64)
				fp++
			} else {
				f.ld64([]Reg{X0, X1, X2, X3, X4, X5, X6, X7}[gp], SP, f.spillOff(i))
				gp++
			}
		}
	}
	// singleRegResult: every exit already produced the result in X0/V0.
	// No trap-slot protocol on return: the runtime zeroes the trap cell before
	// entry, and a trap never returns through here (handler-jump).
	if f.memSizeReg == X17 {
		// Regional leaves can reuse X27. Restore the module bound for direct
		// callers, including when this leaf grew an alias of primary memory.
		f.ld64(X27, linMemReg, -bdCurBytes)
	}
	f.addRspAt = a.Len()
	a.Movz64(X16, 0, 0) // undo the frame; imm patched after body
	a.Movk64(X16, 0, 1)
	a.AddSPReg(X16)
	if f.makesCalls {
		a.LdpPost(FP, LR, SP, 16) // restore FP/LR
	}
	a.Ret()
	if regallocCheckEnabled {
		f.checkRestoreWrites(returnWrites)
	}
	if profileEnabled {
		f.collectProfileSources(internalOff)
	}
	f.emitTrapStubs()
	f.emitPhasePadding()

	f.elideRegisterOnlyFrame()
	if err := f.patchFrameAdjusts(); err != nil {
		return 0, err
	}
	if hostAdapter {
		if !cachedAdapter {
			f.patchBranch26(adapterCall, internalOff)
			if f.sc != nil {
				f.sc.adapterTemplate.observe(f.ft, f.memSizeReg, f.a.B[:internalOff], f.adapterReturnOff, f.adapterEndOff)
			}
		}
		if diagnosticsEnabled && f.stats != nil {
			f.stats.NativeSize.HostAdapterShapeHash = shared.AdapterShapeHash(f.a.B[:f.stats.NativeSize.HostAdapterBytes], adapterCall, 4)
			f.stats.NativeSize.HostAdapterTailBytes = f.stats.NativeSize.HostAdapterBytes - f.adapterReturnOff
			f.stats.NativeSize.HostAdapterTailShapeHash = shared.AdapterShapeHash(f.a.B[f.adapterReturnOff:f.stats.NativeSize.HostAdapterBytes], -1, 0)
		}
	}
	return internalOff, nil
}

func (f *fn) emitPhasePadding() {
	if !f.opt(optOmitColdPhasePadding) {
		for range f.phasePadWords {
			f.a.Nop()
		}
	}
	f.phasePadWords = 0
}

// epilogue: copy results from their canonical slots to the results buffer, restore
// FP/LR (call-making functions), and return. Every reaching path (fallthrough end,
// return, br to the function label) has already placed the results in slots
// [0, resultN).
func (f *fn) epilogue() {
	// Every reaching path has already placed results in canonical slots.
	var returnWrites regallocWriteMask
	if regallocCheckEnabled {
		returnWrites = f.checkTerminalWrites()
	}
	a := f.a
	f.storeModuleGlobals(X2)     // Go exit: module-pinned registers → cells
	f.ld64(X1, SP, frResultsOff) // results ptr (X1 is free at the epilogue)
	resSlot := 0
	out := int32(0)
	for _, rt := range f.ft.Results {
		if mtOf(rt) == mtV128 {
			a.VMovdquLoadDisp(0, SP, f.spillOff(resSlot))
			a.VMovdquStoreDisp(X1, out, 0)
			resSlot += 2
		} else {
			f.ld64(X0, SP, f.spillOff(resSlot))
			f.st64(X1, out, X0)
			resSlot++
		}
		out += abiValSize(rt)
	}
	f.addRspAt = a.Len()
	a.Movz64(X16, 0, 0) // undo the frame; imm patched after body
	a.Movk64(X16, 0, 1)
	a.AddSPReg(X16)
	if f.makesCalls {
		a.LdpPost(FP, LR, SP, 16) // restore FP/LR
	}
	a.Ret()
	if regallocCheckEnabled {
		f.checkRestoreWrites(returnWrites)
	}
}

func abiValSize(t wasm.ValType) int32 {
	if wasm.EqualValType(t, wasm.V128) {
		return 16
	}
	return 8
}

func mtOf(t wasm.ValType) machineType {
	switch {
	case wasm.EqualValType(t, wasm.I32):
		return mtI32
	case wasm.EqualValType(t, wasm.I64):
		return mtI64
	case wasm.EqualValType(t, wasm.F32):
		return mtF32
	case wasm.EqualValType(t, wasm.F64):
		return mtF64
	case wasm.EqualValType(t, wasm.V128):
		return mtV128
	case t.Kind() == wasm.ValRef:
		return mtI64
	}
	return mtNone
}

func countLocals(params []wasm.ValType, locals wasm.Locals) (int, error) {
	n := len(params)
	for _, run := range locals.Runs {
		n += int(run.Count)
	}
	return n, nil
}

// zeroEHGCRootLanes clears the GC lanes of every exception root slot at entry.
// Those lanes are fixed collector roots at every safepoint of the frame, before
// any try_table has initialized its slot.
func (f *fn) zeroEHGCRootLanes() {
	if f.ehRootCap == 0 || f.gcFrameRoots == nil || !f.gcFrameRoots.HasFixedOffsets() {
		return
	}
	for root := 0; root < f.ehRootCap; root++ {
		for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
			f.st64(SP, f.ehRootOff(root)+8+int32(lane)*8, ZR)
		}
	}
}
