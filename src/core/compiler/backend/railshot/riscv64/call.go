//go:build linux && !tinygo

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

const callFrameHeader = 48
const maxDirectCalls = 1024

type directCall struct {
	site   int
	callee uint32
}

// Shared callees can allocate A0..A3. Caller homes and this saved header are
// authoritative across calls; X24 is reserved and unchanged in this same-instance
// ABI. Every callee consumes the shared serialized arena before it can nest.
func (f *integerEmitter) restoreCallTransport() {
	f.a.Ld(enc.X10, enc.SP, 16)
	f.a.Ld(enc.X11, enc.SP, 8)
	f.a.Ld(enc.X12, enc.SP, 24)
	f.a.Ld(enc.X13, enc.SP, 32)
}
func (f *integerEmitter) directCall(index uint32, sig functionSpec, base int) {
	f.restoreCallTransport()
	for j, wide := range sig.params {
		f.Load(5, f.SpillOffset(base+j), wide)
		// i32 loads are already zero-normalized; fill the complete arena cell.
		f.a.Sd(enc.X5, enc.X10, int32(j*8))
	}
	f.calls = append(f.calls, directCall{site: f.a.JumpLong(true), callee: index})
	f.restoreCallTransport()
	f.a.Lwu(enc.X7, enc.X12, 0)
	f.propagate = append(f.propagate, f.a.BranchLong(enc.NE, enc.X7, enc.Zero))
	if sig.result64 {
		f.a.Ld(enc.X5, enc.X11, 0)
	} else {
		f.a.Lwu(enc.X5, enc.X11, 0)
	}
	f.Store(f.SpillOffset(base), 5, sig.result64)
}
