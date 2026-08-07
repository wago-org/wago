//go:build amd64

package amd64

import (
	"fmt"

	railshared "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) emitFE(r *wasm.Reader) error {
	d, err := railshared.DecodeAtomic(r)
	if err != nil {
		return err
	}
	switch {
	case d.Class == railshared.AtomicFence:
		f.materializePendingLoads()
		f.invalidateStoreForward()
		f.a.Mfence()
		return nil
	case d.Class == railshared.AtomicLoad:
		return f.atomicLoad(d)
	case d.Class == railshared.AtomicStore:
		return f.atomicStore(d)
	case d.Class == railshared.AtomicRMW && (d.Operation == railshared.AtomicAdd || d.Operation == railshared.AtomicXchg):
		return f.atomicRMWNative(d)
	default:
		return fmt.Errorf("amd64: unsupported 0xFE opcode %d", d.Sub)
	}
}

func (f *fn) atomicMem(off uint64, size int) (base, ea Reg, disp int32) {
	base, ea, disp = f.indexedMemAddr(0, off, size)
	if size > 1 {
		addr := f.allocReg(maskOf(base).add(ea))
		f.a.LeaScaled(addr, base, ea, 0, disp)
		f.a.TestImm(addr, uint32(size-1), false)
		f.trapIf(condNE, trapAtomicUnaligned)
		f.release(addr)
	}
	return
}

func (f *fn) atomicLoad(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	base, ea, disp := f.atomicMem(d.Offset, int(d.Size))
	out := f.allocReg(maskOf(base).add(ea))
	f.a.LoadIdx(out, base, ea, disp, int(d.Size), false, d.ResultSize == 8)
	f.release(base)
	f.release(ea)
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
	base, ea, disp := f.atomicMem(d.Offset, int(d.Size))
	f.a.XchgIdx(base, ea, value, disp, int(d.Size))
	f.release(base)
	f.release(ea)
	f.pinned = f.pinned.remove(value)
	f.release(value)
	return nil
}

func (f *fn) atomicRMWNative(d railshared.Atomic) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	value := f.materialize(f.popValue())
	f.pinned = f.pinned.add(value)
	base, ea, disp := f.atomicMem(d.Offset, int(d.Size))
	if d.Operation == railshared.AtomicAdd {
		f.a.LockXaddIdx(base, ea, value, disp, int(d.Size))
	} else {
		f.a.XchgIdx(base, ea, value, disp, int(d.Size))
	}
	f.release(base)
	f.release(ea)
	f.pinned = f.pinned.remove(value)
	if d.Size == 1 {
		f.a.Movzx8(value, value, d.ResultSize == 8)
	} else if d.Size == 2 {
		f.a.Movzx16(value, value, d.ResultSize == 8)
	}
	if d.ResultSize == 8 {
		f.pushReg(value, mtI64)
	} else {
		f.pushReg(value, mtI32)
	}
	return nil
}
