package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// MergeNextUseFuel bounds the post-merge semantic scan shared by targets.
const MergeNextUseFuel = 64

// MergeLocalCandidate maps one Wasm local to its target register bank and bit.
// Backends build candidates in fixed stack storage from target-owned state.
type MergeLocalCandidate struct {
	Local uint32
	Reg   uint8
	FP    bool
}

// ScanForwardMergeDeadLocals proves which candidate registers do not need an
// eager merge reload. It owns the architecture-neutral opcode, barrier, fuel,
// and physical-function-end policy. Malformed input or an uncertain boundary
// returns ok=false and no dead registers.
func ScanForwardMergeDeadLocals(r *wasm.Reader, localBase int, candidates []MergeLocalCandidate) (deadGP, deadFP uint64, ok bool) {
	if r == nil || localBase < 0 || uint64(localBase) > uint64(^uint32(0)) || len(candidates) == 0 || len(candidates) > 64 {
		return 0, 0, false
	}
	for _, candidate := range candidates {
		if candidate.Reg >= 64 {
			return 0, 0, false
		}
	}
	active := ^uint64(0)
	if len(candidates) < 64 {
		active = uint64(1)<<uint(len(candidates)) - 1
	}
	base := uint32(localBase)
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
		case 0x02, 0x03, 0x04, 0x05, 0x08, 0x0a, 0x0c, 0x0d, 0x0e, 0x1f:
			return 0, 0, false // structured or exceptional control
		case 0x10, 0x11, 0x12, 0x13, 0x14, 0x15:
			return 0, 0, false // calls may inline or transfer
		case 0x40, 0xfb, 0xfc, 0xfe:
			return 0, 0, false // helper, safepoint, bulk, or atomic barrier
		case 0x20, 0x21, 0x22: // local.get / local.set / local.tee
			x, err := peek.U32()
			if err != nil || x > ^uint32(0)-base {
				return 0, 0, false
			}
			x += base
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
