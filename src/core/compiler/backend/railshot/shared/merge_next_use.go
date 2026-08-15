package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	MaxMergeRegionHints = 8
	MaxMergeRegionBody  = 4096
)

// MergeRegionHints stores eight body offsets as packed nonzero uint16 values.
// Both targets use the same capacity, encoding, overflow, and lookup policy.
type MergeRegionHints [MaxMergeRegionHints / 2]uint32

func (h *MergeRegionHints) Note(start int) {
	if h == nil || start < 0 || start > MaxMergeRegionBody {
		return
	}
	encoded := uint32(start + 1)
	for i := 0; i < MaxMergeRegionHints; i++ {
		word, shift := i/2, uint((i&1)*16)
		if h[word]>>shift&0xffff == 0 {
			h[word] |= encoded << shift
			return
		}
	}
}

func (h MergeRegionHints) Has(start int) bool {
	if start < 0 || start > MaxMergeRegionBody {
		return false
	}
	want := uint32(start + 1)
	for i := 0; i < MaxMergeRegionHints; i++ {
		word, shift := i/2, uint((i&1)*16)
		got := h[word] >> shift & 0xffff
		if got == want {
			return true
		}
		if got == 0 {
			return false
		}
	}
	return false
}

// MergeNextUseFuel bounds the post-merge semantic scan shared by both targets.
const MergeNextUseFuel = 64

// MergeLocalCandidate maps one Wasm local to its target register bank and bit.
// Callers build these in fixed stack storage from target-owned residency state.
type MergeLocalCandidate struct {
	Local uint32
	Reg   uint8
	FP    bool
}

// ScanForwardMergeDeadLocals proves which candidate registers do not need an
// eager merge reload. It owns the architecture-neutral opcode, barrier, fuel,
// and physical-function-end policy. malformed input, capacity overflow, or an
// uncertain boundary returns ok=false and no dead registers.
func ScanForwardMergeDeadLocals(r *wasm.Reader, localBase int, candidates []MergeLocalCandidate) (deadGP, deadFP uint64, ok bool) {
	if r == nil || len(candidates) == 0 || len(candidates) > 64 {
		return 0, 0, false
	}
	active := uint64(1)<<uint(len(candidates)) - 1
	peek := *r
	for fuel := 0; fuel < MergeNextUseFuel; fuel++ {
		op, err := peek.Byte()
		if err != nil {
			return 0, 0, false
		}
		switch op {
		case 0x00, 0x0f: // unreachable / return
			deadGP, deadFP = addActiveMergeCandidates(deadGP, deadFP, active, candidates)
			return deadGP, deadFP, true
		case 0x0b: // only the physical function end proves every candidate dead
			if localBase == 0 && peek.BytesLeft() == 0 {
				deadGP, deadFP = addActiveMergeCandidates(deadGP, deadFP, active, candidates)
				return deadGP, deadFP, true
			}
			return 0, 0, false
		case 0x02, 0x03, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x1f:
			return 0, 0, false // structured or exceptional control
		case 0x10, 0x11, 0x12, 0x13, 0x14, 0x15:
			return 0, 0, false // calls may inline or transfer
		case 0x40, 0xfb, 0xfc, 0xfe:
			return 0, 0, false // helper, safepoint, bulk, or atomic barrier
		case 0x20, 0x21, 0x22: // local.get / local.set / local.tee
			x, err := peek.U32()
			if err != nil {
				return 0, 0, false
			}
			x += uint32(localBase)
			for i, candidate := range candidates {
				bit := uint64(1) << uint(i)
				if active&bit == 0 || candidate.Local != x {
					continue
				}
				if op != 0x20 {
					if candidate.FP {
						deadFP |= uint64(1) << candidate.Reg
					} else {
						deadGP |= uint64(1) << candidate.Reg
					}
				}
				active &^= bit
				break
			}
			if active == 0 {
				return deadGP, deadFP, true
			}
		default:
			if err := wasm.SkipInstructionImmediate(&peek, op); err != nil {
				return 0, 0, false
			}
		}
	}
	return 0, 0, false
}

func addActiveMergeCandidates(deadGP, deadFP, active uint64, candidates []MergeLocalCandidate) (uint64, uint64) {
	for i, candidate := range candidates {
		if active&(uint64(1)<<uint(i)) == 0 {
			continue
		}
		if candidate.FP {
			deadFP |= uint64(1) << candidate.Reg
		} else {
			deadGP |= uint64(1) << candidate.Reg
		}
	}
	return deadGP, deadFP
}

// StackFenceElisionValid validates the shared post-lowering retry condition.
func StackFenceElisionValid(skip bool, finalFrameBytes int) bool {
	return !skip || finalFrameBytes <= 4096
}

// ShouldSkipStackFence applies the shared conservative pre-lowering estimate.
func ShouldSkipStackFence(hasCall bool, frameHeaderBytes, localSlots, bodyBytes int) bool {
	return !hasCall && frameHeaderBytes+8*localSlots+8*bodyBytes <= 4096
}

// CompileRetryState owns the common bounded retry order: first restore a
// conservatively emitted stack fence, then retry once without local pinning.
type CompileRetryState struct {
	PinLocals      bool
	AllowFenceSkip bool
}

func NewCompileRetryState(pinLocals bool) CompileRetryState {
	return CompileRetryState{PinLocals: pinLocals, AllowFenceSkip: true}
}

// Retry updates the state for one failed attempt. Fence recovery precedes
// register-pressure recovery so at most three attempts are possible.
func (s *CompileRetryState) Retry(fenceRequired, registersExhausted bool) bool {
	if fenceRequired && s.AllowFenceSkip {
		s.AllowFenceSkip = false
		return true
	}
	if registersExhausted && s.PinLocals {
		s.PinLocals = false
		return true
	}
	return false
}
