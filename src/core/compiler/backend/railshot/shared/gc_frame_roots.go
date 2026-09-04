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

// GCFrameCallsitePlan names caller-frame roots at the native return PC after a
// direct self-call. ReturnOffset is function-relative and stable in serialized
// artifacts.
type GCFrameCallsitePlan struct {
	ReturnOffset uint32
	StackAdjust  uint32
	Offsets      []uint32
}

// GCFrameRootPlan is an optional compile-time handshake for exact-typed native
// roots in one candidate function. The caller precomputes collector-reference
// local indexes/offsets; architecture backends add site-specific operand spills,
// callsites, and final frame size. It remains compile-only until flattened into
// the validated codec metadata.
type GCFrameRootPlan struct {
	Candidate          bool
	Exact              bool
	FrameBytes         uint32
	LocalIndexes       []uint32
	LocalOffsets       []uint32
	FixedOffsets       []uint32 // conservative always-live roots such as EH payload records
	LiveLocalMasks     []uint64 // low word per reachable allocating site
	LiveCallLocalMasks []uint64 // low word per reachable native call
	LiveMaskExtraWords []uint64 // flat remaining words: allocations, then calls
	// SafepointData stores each allocating site's root offsets as a count word
	// followed by that site's offsets. One pointer-free arena replaces one slice
	// header and backing allocation per site; safepointCount is the checked number
	// of records in the stream.
	SafepointData  []uint32
	safepointCount uint32
	Callsites      []GCFrameCallsitePlan
	// AdapterReturnOffset is relative to the function's public Entry. It may
	// point beyond the function-owned bytes into a module-level adapter island.
	AdapterReturnOffset uint32
	SafepointBase       uint32
}

// ResetSafepoints retains the flat arena for another compile attempt.
func (p *GCFrameRootPlan) ResetSafepoints() {
	p.SafepointData = p.SafepointData[:0]
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
	pos := 0
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
	i := sort.Search(len(p.LocalIndexes), func(i int) bool { return p.LocalIndexes[i] >= index })
	return i < len(p.LocalIndexes) && p.LocalIndexes[i] == index
}

// GCModuleFrameRootPlan owns one independent function plan per local function.
// Distinct entries allow parallel code generation without shared mutation.
type GCModuleFrameRootPlan struct {
	Functions  []*GCFrameRootPlan
	Diagnostic string // compile-only fail-closed admission explanation
}

func (p *GCFrameRootPlan) rootMaskExtraWordsPerSite() int {
	if p == nil || len(p.LocalOffsets) <= 64 {
		return 0
	}
	return (len(p.LocalOffsets)+63)/64 - 1
}

// ValidLiveMasks reports whether the flat extra-word arena matches both
// low-word streams and the function's bounded collector-local count.
func (p *GCFrameRootPlan) ValidLiveMasks() bool {
	if p == nil || len(p.LocalOffsets) > GCFrameTrackedLocalLimit {
		return false
	}
	extra := p.rootMaskExtraWordsPerSite()
	return len(p.LiveMaskExtraWords) == (len(p.LiveLocalMasks)+len(p.LiveCallLocalMasks))*extra
}

func rootMaskContains(low []uint64, extra []uint64, extraPerSite, site, root int) bool {
	if site < 0 || site >= len(low) || root < 0 {
		return false
	}
	word, bit := root/64, uint(root%64)
	if word == 0 {
		return low[site]&(uint64(1)<<bit) != 0
	}
	index := site*extraPerSite + word - 1
	return word <= extraPerSite && index >= 0 && index < len(extra) && extra[index]&(uint64(1)<<bit) != 0
}

// LocalLiveAt reports whether collector local root is live at allocating site.
func (p *GCFrameRootPlan) LocalLiveAt(site, root int) bool {
	return p != nil && root < len(p.LocalOffsets) && rootMaskContains(p.LiveLocalMasks, p.LiveMaskExtraWords, p.rootMaskExtraWordsPerSite(), site, root)
}

// CallLocalLiveAt reports whether collector local root is live at native call.
func (p *GCFrameRootPlan) CallLocalLiveAt(site, root int) bool {
	if p == nil || root >= len(p.LocalOffsets) {
		return false
	}
	extra := p.rootMaskExtraWordsPerSite()
	start := len(p.LiveLocalMasks) * extra
	if start > len(p.LiveMaskExtraWords) {
		return false
	}
	return rootMaskContains(p.LiveCallLocalMasks, p.LiveMaskExtraWords[start:], extra, site, root)
}

// VisitLiveLocals calls visit with each retained-slice index live at one
// allocation or call site. It iterates set mask bits, so sparse sites do work
// proportional to their live population instead of all retained locals.
func (p *GCFrameRootPlan) VisitLiveLocals(site int, call bool, visit func(root int)) bool {
	if p == nil || visit == nil || len(p.LocalIndexes) != len(p.LocalOffsets) {
		return false
	}
	masks := p.LiveLocalMasks
	extraPerSite := p.rootMaskExtraWordsPerSite()
	extraStart := 0
	if call {
		masks = p.LiveCallLocalMasks
		extraStart = len(p.LiveLocalMasks) * extraPerSite
	}
	if site < 0 || site >= len(masks) {
		return false
	}
	visitWord := func(word int, value uint64) bool {
		for value != 0 {
			bit := bits.TrailingZeros64(value)
			value &= value - 1
			root := word*64 + bit
			if root >= len(p.LocalOffsets) {
				return false
			}
			visit(root)
		}
		return true
	}
	if !visitWord(0, masks[site]) {
		return false
	}
	base := extraStart + site*extraPerSite
	if base < 0 || base+extraPerSite > len(p.LiveMaskExtraWords) {
		return false
	}
	for word := 1; word <= extraPerSite; word++ {
		if !visitWord(word, p.LiveMaskExtraWords[base+word-1]) {
			return false
		}
	}
	return true
}

func (p *GCModuleFrameRootPlan) Function(index int) *GCFrameRootPlan {
	if p == nil || index < 0 || index >= len(p.Functions) {
		return nil
	}
	return p.Functions[index]
}
