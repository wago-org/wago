//go:build arm64

package arm64

import (
	"fmt"

	railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// emitFE lowers the core threads proposal's 0xfe instruction family. Keep this
// dispatch exhaustive and fail closed while the family is landed incrementally.
func (f *fn) emitFE(r *wasm.Reader) error {
	d, err := railshared.DecodeAtomic(r)
	if err != nil {
		return err
	}
	switch {
	case d.Class == railshared.AtomicFence:
		f.materializePendingLoads()
		f.invalidateStoreForward()
		f.a.DmbIsh()
		return nil
	case d.Class == railshared.AtomicLoad:
		return f.atomicLoad(d)
	case d.Class == railshared.AtomicStore:
		return f.atomicStore(d)
	case d.Class == railshared.AtomicRMW:
		return f.atomicRMW(d)
	case d.Class == railshared.AtomicCmpxchg:
		return f.atomicCmpxchg(d)
	default:
		return fmt.Errorf("arm64: unsupported 0xFE opcode %d", d.Sub)
	}
}

func (f *fn) atomicCmpxchg(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	replacement := f.materialize(f.popValue())
	f.pinned = f.pinned.add(replacement)
	expected := f.materialize(f.popValue())
	f.pinned = f.pinned.add(expected)

	compare := expected
	if d.Size < d.ResultSize {
		compare = f.allocReg(maskOf(expected, replacement))
		mask := uint64(0xff)
		if d.Size == 2 {
			mask = 0xffff
		} else if d.Size == 4 {
			mask = 0xffffffff
		}
		if d.ResultSize == 8 {
			f.a.AndImm64(compare, expected, mask)
		} else {
			f.a.AndImm32(compare, expected, uint32(mask))
		}
		f.pinned = f.pinned.add(compare)
	}
	addr := f.atomicAddr(d.Offset, int(d.Size))
	old := f.allocReg(maskOf(addr, replacement, expected, compare))
	status := f.allocReg(maskOf(addr, replacement, expected, compare, old))
	loop := f.a.Len()
	f.a.Ldaxr(old, addr, int(d.Size))
	f.cmpRR(old, compare, d.ResultSize == 8)
	notEqual := f.a.Bcond(condNE)
	f.a.Stlxr(status, replacement, addr, int(d.Size))
	f.a.PatchBranch19(f.a.Cbnz64(status), loop)
	done := f.a.Branch()
	f.a.PatchBranch19(notEqual, f.a.Len())
	f.a.Clrex()
	f.a.PatchBranch26(done, f.a.Len())

	f.release(status)
	f.release(addr)
	if compare != expected {
		f.pinned = f.pinned.remove(compare)
		f.release(compare)
	}
	f.pinned = f.pinned.remove(expected).remove(replacement)
	f.release(expected)
	f.release(replacement)
	if d.ResultSize == 8 {
		f.pushReg(old, mtI64)
	} else {
		f.pushReg(old, mtI32)
	}
	return nil
}

func (f *fn) atomicAddr(off uint64, size int) Reg {
	base, ea, disp := f.indexedMemAddr(0, off, size)
	addr := f.allocReg(maskOf(base, ea))
	f.a.Add64(addr, base, ea)
	f.addDisp(addr, addr, disp, true)
	f.release(base)
	f.release(ea)
	if size > 1 {
		if !f.a.TstImm64(addr, uint64(size-1)) {
			panic("arm64: atomic alignment mask is not encodable")
		}
		f.trapIf(condNE, trapAtomicUnaligned)
	}
	return addr
}

func (f *fn) atomicLoad(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	addr := f.atomicAddr(d.Offset, int(d.Size))
	out := f.allocReg(maskOf(addr))
	f.a.Ldar(out, addr, int(d.Size))
	f.release(addr)
	if d.ResultSize == 8 {
		f.pushReg(out, mtI64)
	} else {
		f.pushReg(out, mtI32)
	}
	return nil
}

func (f *fn) atomicStore(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	value := f.materialize(f.popValue())
	f.pinned = f.pinned.add(value)
	addr := f.atomicAddr(d.Offset, int(d.Size))
	f.a.Stlr(value, addr, int(d.Size))
	f.release(addr)
	f.pinned = f.pinned.remove(value)
	f.release(value)
	return nil
}

func (f *fn) atomicRMW(d railshared.Atomic) error {
	// Atomics are observable memory barriers: realize deferred reads and discard
	// the scalar store-forwarding window before entering the exclusive loop.
	f.materializePendingLoads()
	f.invalidateStoreForward()

	value := f.popValue()
	vreg, vOwned := f.materializeRead(value)
	f.pinned = f.pinned.add(vreg)
	addr := f.atomicAddr(d.Offset, int(d.Size))

	old := f.allocReg(maskOf(vreg, addr))
	next := f.allocReg(maskOf(vreg, addr, old))
	status := f.allocReg(maskOf(vreg, addr, old, next))
	loop := f.a.Len()
	f.a.Ldaxr(old, addr, int(d.Size))
	wide := d.ResultSize == 8
	switch d.Operation {
	case railshared.AtomicAdd:
		if wide {
			f.a.Add64(next, old, vreg)
		} else {
			f.a.Add32(next, old, vreg)
		}
	case railshared.AtomicSub:
		if wide {
			f.a.Sub64(next, old, vreg)
		} else {
			f.a.Sub32(next, old, vreg)
		}
	case railshared.AtomicAnd:
		if wide {
			f.a.And64(next, old, vreg)
		} else {
			f.a.And32(next, old, vreg)
		}
	case railshared.AtomicOr:
		if wide {
			f.a.Orr64(next, old, vreg)
		} else {
			f.a.Orr32(next, old, vreg)
		}
	case railshared.AtomicXor:
		if wide {
			f.a.Eor64(next, old, vreg)
		} else {
			f.a.Eor32(next, old, vreg)
		}
	case railshared.AtomicXchg:
		if wide {
			f.a.MovReg64(next, vreg)
		} else {
			f.a.MovReg32(next, vreg)
		}
	default:
		return fmt.Errorf("arm64: unsupported atomic RMW operation %d", d.Operation)
	}
	f.a.Stlxr(status, next, addr, int(d.Size))
	f.a.PatchBranch19(f.a.Cbnz64(status), loop)

	f.release(status)
	f.release(next)
	f.release(addr)
	f.pinned = f.pinned.remove(vreg)
	if vOwned {
		f.release(vreg)
	}
	if wide {
		f.pushReg(old, mtI64)
	} else {
		f.pushReg(old, mtI32)
	}
	return nil
}
