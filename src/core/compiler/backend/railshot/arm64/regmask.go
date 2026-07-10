//go:build arm64

package arm64

import "math/bits"

// regMask is a bitset of physical registers, one bit per register (GPRs occupy
// bits 0..30; V (SIMD/FP) registers, when added later, occupy bits 32..63). It
// backs the allocator's occupancy/protection sets. Ported from WARP's RegMask
// (uint64 bitset with add/unmask/contains/all/none/count).
type regMask uint64

func maskOf(regs ...Reg) regMask {
	var m regMask
	for _, r := range regs {
		m |= 1 << uint(r)
	}
	return m
}

func (m regMask) has(r Reg) bool { return m&(1<<uint(r)) != 0 }
func (m regMask) add(r Reg) regMask {
	return m | 1<<uint(r)
}
func (m regMask) remove(r Reg) regMask { return m &^ (1 << uint(r)) }
func (m regMask) union(o regMask) regMask {
	return m | o
}
func (m regMask) count() int { return bits.OnesCount64(uint64(m)) }

// firstIn returns the first register from order that is present in m, or ok=false.
func (m regMask) firstIn(order []Reg) (Reg, bool) {
	for _, r := range order {
		if m.has(r) {
			return r, true
		}
	}
	return 0, false
}
