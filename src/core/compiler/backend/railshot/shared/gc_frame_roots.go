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
	Candidate           bool
	Exact               bool
	FrameBytes          uint32
	LocalIndexes        []uint32
	LocalOffsets        []uint32
	FixedOffsets        []uint32 // conservative always-live roots such as EH payload records
	LiveLocalMasks      []uint64 // one exact local-liveness mask per reachable allocating site
	LiveCallLocalMasks  []uint64 // one exact local-liveness mask per reachable direct self-call
	Safepoints          []GCFrameSafepointPlan
	Callsites           []GCFrameCallsitePlan
	AdapterReturnOffset uint32
	SafepointBase       uint32
}

// GCModuleFrameRootPlan owns one independent function plan per local function.
// Distinct entries allow parallel code generation without shared mutation.
type GCModuleFrameRootPlan struct {
	Functions []*GCFrameRootPlan
}

func (p *GCModuleFrameRootPlan) Function(index int) *GCFrameRootPlan {
	if p == nil || index < 0 || index >= len(p.Functions) {
		return nil
	}
	return p.Functions[index]
}
