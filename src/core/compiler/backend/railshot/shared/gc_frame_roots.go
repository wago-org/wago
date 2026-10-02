package shared

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

	// GCFrameRootLimit bounds exact roots in one native frame. The compiler keeps
	// a one-word fast path through 64 roots, a two-word path through 128 roots,
	// and uses one flat word arena for larger masks up to this limit.
	GCFrameRootLimit = 1024
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

// GCFrameSafepointPlan names the direct mutable native slots visible at one
// allocating helper transition. Offsets are relative to post-prologue RSP and
// include admitted local slots followed by live operand spill slots.
type GCFrameSafepointPlan struct {
	ID      uint32
	Offsets []uint32
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
	Safepoints         []GCFrameSafepointPlan
	Callsites          []GCFrameCallsitePlan
	// AdapterReturnOffset is relative to the function's public Entry. It may
	// point beyond the function-owned bytes into a module-level adapter island.
	AdapterReturnOffset uint32
	SafepointBase       uint32
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
	if p == nil || len(p.LocalOffsets) > GCFrameRootLimit {
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

func (p *GCModuleFrameRootPlan) Function(index int) *GCFrameRootPlan {
	if p == nil || index < 0 || index >= len(p.Functions) {
		return nil
	}
	return p.Functions[index]
}
