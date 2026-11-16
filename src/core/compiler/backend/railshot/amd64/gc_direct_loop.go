//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Separate from handle certificates: null is represented by object==0 and is
// NOT validated at loop entry. These registers are not roots. The bounded
// scanner proves no safepoint and no receiver assignment until endPC.
// A fixed 16-byte compiler state avoids per-op maps or heap allocations.
type directLoopLease struct {
	endPC, local, typeIndex uint32
	object, length          Reg
	remaining               uint16
}

func (f *fn) prepareDirectLoop(op byte) {
	if f.wasmPC >= f.directLoop.endPC {
		if f.directLoop.remaining != 0 {
			panic("amd64: direct loop access proof not consumed")
		}
		f.pinned = f.pinned.remove(f.directLoop.object).remove(f.directLoop.length)
		f.directLoop = directLoopLease{}
		return
	}
	switch op {
	case 0x1a, 0x20, 0x21, 0x22, 0x41, 0x42, 0xfb, 0x0d,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		return
	default:
		panic("amd64: effect inside direct loop lease")
	}
}

func (f *fn) hoistDirectLoop(reader *wasm.Reader) {
	if !gcTrustedDirectLoop || f.directLoop.endPC != 0 {
		return
	}
	p, ok := f.planGCLoopResolverMode(reader, true)
	if !ok {
		f.stats.peep("gc-direct-loop-no-plan")
		return
	}
	obj := f.directLoopRegister()
	if obj == regNone {
		return
	}
	f.pinned = f.pinned.add(obj)
	length := f.directLoopRegister()
	if length == regNone {
		f.stats.peep("gc-direct-loop-no-length-reg")
		f.pinned = f.pinned.remove(obj)
		return
	}
	f.pinned = f.pinned.add(length)
	f.pushPlainLocalGet(uint32(p.local))
	ref := f.materialize(f.popValue())
	f.a.MovRegReg32(obj, ref)
	f.release(ref)
	f.a.MovImm32(length, 0)
	f.a.TestSelf(obj, false)
	null := f.a.JccPlaceholder(condE)
	// The attested non-null reference is live and typed. Loading Aux is safe,
	// but null/bounds traps stay at each original access, not in this prelude.
	f.a.Load64(length, RBX, -int32(abi.GCNativeViewPtrOffset))
	f.a.Load64(length, length, gc.DirectInstanceCollectorOffset)
	f.a.Load64(length, length, gc.DirectViewArenaOffset)
	f.a.Add64(obj, length)
	f.a.Load32(length, obj, 8)
	f.a.PatchRel32(null, f.a.Len())
	f.directLoop = directLoopLease{p.endPC, uint32(p.local), p.typeIndex, obj, length, p.accesses}
	f.stats.peep("gc-direct-loop-hoist")
}

// No admitted instruction has a fixed pointer/scratch use of these registers.
// In particular calls, bulk operations and interrupt polls are excluded.
func (f *fn) directLoopRegister() Reg {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range [...]Reg{RBP, R12, R13, R14, RDI, RSI, R9, R10, R11, R15} {
		if f.regUser[r] == nil && !block.has(r) {
			return r
		}
	}
	return regNone
}
