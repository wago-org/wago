//go:build arm64

package arm64

type intConstReg struct {
	typ  machineType
	bits int64
	reg  Reg
}

// On-the-fly register allocator — the core of WARP's speed. Values (locals,
// temporaries, deferred results) live in registers over the whole general-purpose
// file and are spilled to frame slots only when the allocator runs out. Ported
// from WARP's reqScratchReg / spillFromStack / liftToRegInPlace (Common.cpp) and
// the register occupancy the backend tracks (aarch64_cc.hpp).

const regNone Reg = 0xFF

// spillIfUsed evicts register r's occupant to a frame slot if one is resident,
// freeing r for a fixed-role use. AArch64 has no fixed-register ALU ops (mul/div/
// shift are all orthogonal), so this is needed far less than on x86; it survives
// only to free the X0 result register when it must hold a specific value.
func (f *fn) spillIfUsed(r Reg) {
	if u := f.regUser[r]; u != nil {
		f.spill(u)
	}
}
