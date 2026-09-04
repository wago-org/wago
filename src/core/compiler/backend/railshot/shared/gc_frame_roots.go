package shared

import (
	"math/bits"
	"sort"
)

// AMD64FrameHeaderBytes and ARM64FrameHeaderBytes are the stable local-slot
// bases used by the railshot native frames. Root-map producers and consumers
// must use the matching architecture value.
const (
	AMD64FrameHeaderBytes = 16
	ARM64FrameHeaderBytes = 16

	// ARM64SavedLROffset is the saved LR qword within the 16-byte FP/LR record
	// immediately above a call-making function's reserved frame. Advancing to the
	// caller consumes the complete record.
	ARM64SavedLROffset    = 8
	ARM64FrameRecordBytes = 16
)

const (
	// GCHelperIDBits reserves the low dispatch bits for the stable helper ID.
	// Remaining low-30-bit payload bits identify an allocating safepoint; bits
	// 30-31 remain the existing GC and host-funcref dispatch tags.
	GCHelperIDBits     = 8
	GCHelperIDMask     = uint32(1<<GCHelperIDBits) - 1
	GCSafepointIDShift = GCHelperIDBits
	GCSafepointIDMax   = uint32(1<<(30-GCSafepointIDShift)) - 1

	// GCFrameTrackedLocalLimit is the maximum configured parameter-plus-local
	// population whose liveness may be tracked. Final exact root vectors are
	// variable-sized and remain bounded by the independently validated native
	// frame size and serialized metadata length.
	GCFrameTrackedLocalLimit = 1<<16 - 1
)

func EncodeGCDispatch(helper, safepoint uint32) (uint32, bool) {
	if helper > GCHelperIDMask || safepoint > GCSafepointIDMax {
		return 0, false
	}
	return helper | safepoint<<GCSafepointIDShift, true
}

func DecodeGCDispatch(payload uint32) (helper, safepoint uint32) {
	return payload & GCHelperIDMask, payload >> GCSafepointIDShift
}

// GCFrameRootPlan is an optional compile-time handshake for exact-typed native
// roots in one candidate function. The caller precomputes collector-reference
// local indexes/offsets; architecture backends add site-specific operand spills,
// callsites, and final frame size. It remains compile-only until flattened into
// the validated codec metadata.
type GCFrameRootPlan struct {
	Candidate    bool
	Exact        bool
	Conservative bool // local masks retain every tracked local at each site
	FrameBytes   uint32
	Locals       []GCFrameLocal
	// LiveMaskWords is one site-major, pointer-free arena. Allocating sites come
	// first, followed by native calls; every site occupies rootMaskWordsPerSite
	// words. The explicit counts avoid retaining two more slice headers and
	// backing allocations in every collecting function.
	liveMaskWords       []uint64
	allocationMaskCount uint32
	callMaskCount       uint32
	// SafepointData owns conservative always-live roots as a fixed prefix, then
	// each allocating site's root offsets as a count word followed by that site's
	// offsets. Sharing this pointer-free arena avoids a slice header in every plan;
	// safepointCount is the checked number of records after the fixed prefix.
	SafepointData    []uint32
	fixedOffsetCount uint32
	safepointCount   uint32
	callsiteCount    uint32
	CallsiteData     []uint32
	// AdapterReturnOffset is relative to the function's public Entry. It may
	// point beyond the function-owned bytes into a module-level adapter island.
	AdapterReturnOffset uint32
	SafepointBase       uint32
}

// SetFixedOffsets transfers ownership of the conservative always-live roots to
// the prefix of the safepoint arena. It is valid only before site emission.
func (p *GCFrameRootPlan) SetFixedOffsets(offsets []uint32) bool {
	if p == nil || len(p.SafepointData) != 0 || p.fixedOffsetCount != 0 || p.safepointCount != 0 || p.callsiteCount != 0 || uint64(len(offsets)) > uint64(^uint32(0)) {
		return false
	}
	p.SafepointData = offsets
	p.fixedOffsetCount = uint32(len(offsets))
	return true
}

// FixedOffsets returns the conservative always-live prefix. A malformed
// private count returns nil; VisitSafepoints provides the fail-closed check.
func (p *GCFrameRootPlan) FixedOffsets() []uint32 {
	if p == nil || uint64(p.fixedOffsetCount) > uint64(len(p.SafepointData)) {
		return nil
	}
	return p.SafepointData[:p.fixedOffsetCount:p.fixedOffsetCount]
}

func (p *GCFrameRootPlan) HasFixedOffsets() bool {
	return p != nil && p.fixedOffsetCount != 0
}

// GCFrameLocal keeps one collector-reference local's validated Wasm index and
// mutable native-frame home together in one pointer-free backing array.
type GCFrameLocal struct {
	Index  uint32
	Offset uint32
}

// GCFrameCallsite names caller-frame roots at a native return PC. Its view is
// valid only during VisitCallsites; setters update the owning flat stream.
type GCFrameCallsite struct{ data []uint32 }

func (c GCFrameCallsite) ReturnOffset() uint32     { return c.data[0] }
func (c GCFrameCallsite) SetReturnOffset(v uint32) { c.data[0] = v }
func (c GCFrameCallsite) StackAdjust() uint32      { return c.data[1] }
func (c GCFrameCallsite) Offsets() []uint32        { return c.data[3:] }

func (p *GCFrameRootPlan) ResetCallsites() {
	p.CallsiteData = p.CallsiteData[:0]
	p.callsiteCount = 0
}

// AppendCallsite retains one complete callsite without a per-site slice header
// or offset backing. Return offsets remain mutable through VisitCallsites so
// native finalization can remap them in place.
func (p *GCFrameRootPlan) AppendCallsite(returnOffset, stackAdjust uint32, offsets []uint32) bool {
	if p.callsiteCount == ^uint32(0) || uint64(len(offsets)) > uint64(^uint32(0)) {
		return false
	}
	p.CallsiteData = append(p.CallsiteData, returnOffset, stackAdjust, uint32(len(offsets)))
	p.CallsiteData = append(p.CallsiteData, offsets...)
	p.callsiteCount++
	return true
}

// RecordCallsite is the fail-closed compiler form of AppendCallsite.
func (p *GCFrameRootPlan) RecordCallsite(returnOffset, stackAdjust uint32, offsets []uint32) {
	if !p.AppendCallsite(returnOffset, stackAdjust, offsets) {
		p.Exact = false
	}
}

func (p *GCFrameRootPlan) CallsiteCount() int { return int(p.callsiteCount) }

func (p *GCFrameRootPlan) Callsite(index int) (result GCFrameCallsite, ok bool) {
	if index < 0 || index >= p.CallsiteCount() {
		return result, false
	}
	ok = p.VisitCallsites(func(i int, callsite GCFrameCallsite) bool {
		if i == index {
			result = callsite
			return false
		}
		return true
	})
	// VisitCallsites reports false when the visitor stops at the requested site.
	if len(result.data) != 0 {
		return result, true
	}
	return result, ok
}

func (p *GCFrameRootPlan) VisitCallsites(visit func(index int, callsite GCFrameCallsite) bool) bool {
	if p == nil || visit == nil {
		return false
	}
	pos := 0
	for i := uint32(0); i < p.callsiteCount; i++ {
		if pos+3 > len(p.CallsiteData) {
			return false
		}
		n := uint64(p.CallsiteData[pos+2])
		end := uint64(pos+3) + n
		if end > uint64(len(p.CallsiteData)) || !visit(int(i), GCFrameCallsite{data: p.CallsiteData[pos:int(end)]}) {
			return false
		}
		pos = int(end)
	}
	return pos == len(p.CallsiteData)
}

// ShiftCallsiteReturnOffsets applies a code deletion to every return PC at or
// after start. Malformed streams and underflow fail exact-root admission.
func (p *GCFrameRootPlan) ShiftCallsiteReturnOffsets(start, deleted uint32) bool {
	ok := p.VisitCallsites(func(_ int, callsite GCFrameCallsite) bool {
		off := callsite.ReturnOffset()
		if off < start {
			return true
		}
		if off < deleted {
			return false
		}
		callsite.SetReturnOffset(off - deleted)
		return true
	})
	if !ok {
		p.Exact = false
	}
	return ok
}

// ResetSafepoints retains the fixed root prefix and flat arena for another
// compile attempt.
func (p *GCFrameRootPlan) ResetSafepoints() {
	if uint64(p.fixedOffsetCount) > uint64(len(p.SafepointData)) {
		p.SafepointData = nil
		p.fixedOffsetCount = 0
		p.Exact = false
	} else {
		p.SafepointData = p.SafepointData[:p.fixedOffsetCount]
	}
	p.safepointCount = 0
}

// GCFrameSafepointBuilder owns one incomplete record in a root plan. Its
// unexported state prevents callers from committing or aborting an arbitrary
// prefix of the flat stream.
type GCFrameSafepointBuilder struct {
	plan  *GCFrameRootPlan
	start int
}

// BeginSafepoint starts one offset record. Abort must be called if the producer
// cannot finish it.
func (p *GCFrameRootPlan) BeginSafepoint() GCFrameSafepointBuilder {
	b := GCFrameSafepointBuilder{plan: p, start: len(p.SafepointData)}
	p.SafepointData = append(p.SafepointData, 0)
	return b
}

func (b *GCFrameSafepointBuilder) AppendOffset(offset uint32) {
	if b.plan != nil {
		b.plan.SafepointData = append(b.plan.SafepointData, offset)
	}
}

func (b *GCFrameSafepointBuilder) Offsets() ([]uint32, bool) {
	if b.plan == nil || b.start < 0 || b.start >= len(b.plan.SafepointData) {
		return nil, false
	}
	return b.plan.SafepointData[b.start+1:], true
}

func (b *GCFrameSafepointBuilder) Commit() bool {
	p := b.plan
	if p == nil || b.start < 0 || b.start >= len(p.SafepointData) || p.safepointCount == ^uint32(0) {
		return false
	}
	n := len(p.SafepointData) - b.start - 1
	if uint64(n) > uint64(^uint32(0)) {
		return false
	}
	p.SafepointData[b.start] = uint32(n)
	p.safepointCount++
	b.plan = nil
	return true
}

func (b *GCFrameSafepointBuilder) Abort() {
	if b.plan != nil && b.start >= 0 && b.start <= len(b.plan.SafepointData) {
		b.plan.SafepointData = b.plan.SafepointData[:b.start]
	}
	b.plan = nil
}

func (p *GCFrameRootPlan) SafepointCount() int { return int(p.safepointCount) }

// AppendSafepoint appends one complete record. Streaming backend producers use
// BeginSafepoint directly to avoid a temporary offsets allocation.
func (p *GCFrameRootPlan) AppendSafepoint(offsets []uint32) bool {
	b := p.BeginSafepoint()
	for _, off := range offsets {
		b.AppendOffset(off)
	}
	if !b.Commit() {
		b.Abort()
		return false
	}
	return true
}

// VisitSafepoints validates and visits the complete flat stream in ID order.
func (p *GCFrameRootPlan) VisitSafepoints(visit func(index int, offsets []uint32) bool) bool {
	if p == nil || visit == nil {
		return false
	}
	if uint64(p.fixedOffsetCount) > uint64(len(p.SafepointData)) {
		return false
	}
	pos := int(p.fixedOffsetCount)
	for i := uint32(0); i < p.safepointCount; i++ {
		if pos >= len(p.SafepointData) {
			return false
		}
		n := uint64(p.SafepointData[pos])
		pos++
		end := uint64(pos) + n
		if end > uint64(len(p.SafepointData)) || !visit(int(i), p.SafepointData[pos:int(end)]) {
			return false
		}
		pos = int(end)
	}
	return pos == len(p.SafepointData)
}

// TracksLocal reports whether index belongs to the sorted collector-local
// population retained by the frontend. Binary search keeps backend local
// instruction handling bounded when disjoint safepoints retain a wide union.
func (p *GCFrameRootPlan) TracksLocal(index uint32) bool {
	if p == nil || !p.Candidate {
		return false
	}
	i := sort.Search(len(p.Locals), func(i int) bool { return p.Locals[i].Index >= index })
	return i < len(p.Locals) && p.Locals[i].Index == index
}

// GCModuleFrameRootPlan owns one independent plan for every collecting local
// function. The pointer-free dense index preserves O(1) lookup without retaining
// one scanned pointer per RootNone function, while the sparse arena removes one
// heap allocation per retained plan. The arena is complete before parallel code
// generation begins, so returned plan addresses remain stable while workers
// mutate distinct entries.
type GCModuleFrameRootPlan struct {
	functionPlanIndexes []uint32 // sparse arena index + 1; zero means RootNone
	functions           []GCFrameRootPlan
}

const pendingGCFrameRootPlan = ^uint32(0)

func NewGCModuleFrameRootPlan(functionCount int) *GCModuleFrameRootPlan {
	if functionCount < 0 {
		functionCount = 0
	}
	return &GCModuleFrameRootPlan{functionPlanIndexes: make([]uint32, functionCount)}
}

// MarkFunction records a semantic non-RootNone decision before the more
// expensive plan pass. This lets the caller reserve the sparse arena exactly
// while using the final dense index itself as the temporary pointer-free
// decision vector.
func (p *GCModuleFrameRootPlan) MarkFunction(index int) bool {
	if p == nil || index < 0 || index >= len(p.functionPlanIndexes) || p.functionPlanIndexes[index] != 0 {
		return false
	}
	p.functionPlanIndexes[index] = pendingGCFrameRootPlan
	return true
}

// FunctionPending reports whether index was classified as non-RootNone but has
// not yet been populated by BeginFunction.
func (p *GCModuleFrameRootPlan) FunctionPending(index int) bool {
	return p != nil && index >= 0 && index < len(p.functionPlanIndexes) && p.functionPlanIndexes[index] == pendingGCFrameRootPlan
}

// ReserveFunctions reserves the sparse plan arena before any function begins.
func (p *GCModuleFrameRootPlan) ReserveFunctions(count int) bool {
	if p == nil || count < 0 || uint64(count) >= uint64(pendingGCFrameRootPlan) || len(p.functions) != 0 || cap(p.functions) != 0 {
		return false
	}
	p.functions = make([]GCFrameRootPlan, 0, count)
	return true
}

// BeginFunction appends one pending plan and returns its stable address. A
// complete reservation is required first: refusing growth makes every address
// returned during module planning stable through parallel code generation.
func (p *GCModuleFrameRootPlan) BeginFunction(index int) (*GCFrameRootPlan, bool) {
	if p == nil || index < 0 || index >= len(p.functionPlanIndexes) || p.functionPlanIndexes[index] != pendingGCFrameRootPlan || len(p.functions) >= cap(p.functions) || uint64(len(p.functions)) >= uint64(pendingGCFrameRootPlan-1) {
		return nil, false
	}
	p.functions = append(p.functions, GCFrameRootPlan{})
	p.functionPlanIndexes[index] = uint32(len(p.functions))
	return &p.functions[len(p.functions)-1], true
}

// FunctionCount returns the local-function population, including RootNone
// entries. It is the dense iteration bound for callers that need source indexes.
func (p *GCModuleFrameRootPlan) FunctionCount() int {
	if p == nil {
		return 0
	}
	return len(p.functionPlanIndexes)
}

func (p *GCFrameRootPlan) rootMaskWordsPerSite() int {
	if p == nil || len(p.Locals) == 0 {
		return 1
	}
	return (len(p.Locals) + 63) / 64
}

// AllocationMaskCount returns the number of exact allocating-site masks.
func (p *GCFrameRootPlan) AllocationMaskCount() int {
	if p == nil {
		return 0
	}
	return int(p.allocationMaskCount)
}

// CallMaskCount returns the number of logical Wasm-call masks. One logical
// call may later produce multiple native return-path callsites.
func (p *GCFrameRootPlan) CallMaskCount() int {
	if p == nil {
		return 0
	}
	return int(p.callMaskCount)
}

// SetLiveMasks installs one complete site-major mask arena.
func (p *GCFrameRootPlan) SetLiveMasks(words []uint64, allocationSites, callSites int) bool {
	if p == nil || allocationSites < 0 || callSites < 0 || uint64(allocationSites) > uint64(^uint32(0)) || uint64(callSites) > uint64(^uint32(0)) {
		return false
	}
	sites := uint64(allocationSites) + uint64(callSites)
	if len(p.Locals) > GCFrameTrackedLocalLimit || sites*uint64(p.rootMaskWordsPerSite()) != uint64(len(words)) {
		return false
	}
	p.liveMaskWords = words
	p.allocationMaskCount = uint32(allocationSites)
	p.callMaskCount = uint32(callSites)
	return true
}

// ValidLiveMasks reports whether the site-major arena matches both site counts
// and the function's bounded collector-local count.
func (p *GCFrameRootPlan) ValidLiveMasks() bool {
	if p == nil || len(p.Locals) > GCFrameTrackedLocalLimit {
		return false
	}
	sites := uint64(p.allocationMaskCount) + uint64(p.callMaskCount)
	return sites*uint64(p.rootMaskWordsPerSite()) == uint64(len(p.liveMaskWords))
}

func rootMaskContains(words []uint64, wordsPerSite, siteCount, site, root int) bool {
	if site < 0 || site >= siteCount || root < 0 {
		return false
	}
	word, bit := root/64, uint(root%64)
	index := site*wordsPerSite + word
	return word < wordsPerSite && index >= 0 && index < len(words) && words[index]&(uint64(1)<<bit) != 0
}

// LocalLiveAt reports whether collector local root is live at allocating site.
func (p *GCFrameRootPlan) LocalLiveAt(site, root int) bool {
	return p != nil && root < len(p.Locals) && rootMaskContains(p.liveMaskWords, p.rootMaskWordsPerSite(), p.AllocationMaskCount(), site, root)
}

// CallLocalLiveAt reports whether collector local root is live at native call.
func (p *GCFrameRootPlan) CallLocalLiveAt(site, root int) bool {
	if p == nil || root >= len(p.Locals) {
		return false
	}
	wordsPerSite := p.rootMaskWordsPerSite()
	start := p.AllocationMaskCount() * wordsPerSite
	if start < 0 || start > len(p.liveMaskWords) {
		return false
	}
	return rootMaskContains(p.liveMaskWords[start:], wordsPerSite, p.CallMaskCount(), site, root)
}

// VisitLiveLocals calls visit with each retained-slice index live at one
// allocation or call site. It iterates set mask bits, so sparse sites do work
// proportional to their live population instead of all retained locals.
func (p *GCFrameRootPlan) VisitLiveLocals(site int, call bool, visit func(root int)) bool {
	if p == nil || visit == nil {
		return false
	}
	wordsPerSite := p.rootMaskWordsPerSite()
	siteCount := p.AllocationMaskCount()
	start := 0
	if call {
		siteCount = p.CallMaskCount()
		start = p.AllocationMaskCount() * wordsPerSite
	}
	if site < 0 || site >= siteCount {
		return false
	}
	visitWord := func(word int, value uint64) bool {
		for value != 0 {
			bit := bits.TrailingZeros64(value)
			value &= value - 1
			root := word*64 + bit
			if root >= len(p.Locals) {
				return false
			}
			visit(root)
		}
		return true
	}
	base := start + site*wordsPerSite
	if base < 0 || base+wordsPerSite > len(p.liveMaskWords) {
		return false
	}
	for word := 0; word < wordsPerSite; word++ {
		if !visitWord(word, p.liveMaskWords[base+word]) {
			return false
		}
	}
	return true
}

func (p *GCModuleFrameRootPlan) Function(index int) *GCFrameRootPlan {
	if p == nil || index < 0 || index >= len(p.functionPlanIndexes) {
		return nil
	}
	sparse := p.functionPlanIndexes[index]
	if sparse == 0 || uint64(sparse) > uint64(len(p.functions)) {
		return nil
	}
	return &p.functions[int(sparse-1)]
}
