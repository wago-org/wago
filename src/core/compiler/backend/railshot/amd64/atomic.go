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
	case d.Class == railshared.AtomicRMW && d.Operation == railshared.AtomicAdd && d.Size == 4 && d.ResultSize == 4:
		return f.atomicRMWAdd32(d.Offset)
	default:
		return fmt.Errorf("amd64: unsupported 0xFE opcode %d", d.Sub)
	}
}

func (f *fn) atomicRMWAdd32(off uint64) error {
	f.materializePendingLoads()
	f.invalidateStoreForward()
	value := f.materialize(f.popValue())
	f.pinned = f.pinned.add(value)
	base, ea, disp := f.indexedMemAddr(0, off, 4)
	addr := f.allocReg(maskOf(base).add(ea).add(value))
	f.a.LeaScaled(addr, base, ea, 0, disp)
	f.a.TestImm(addr, 3, false)
	f.trapIf(condNE, trapAtomicUnaligned)
	f.release(addr)
	f.a.LockXaddIdx32(base, ea, value, disp)
	f.release(base)
	f.release(ea)
	f.pinned = f.pinned.remove(value)
	f.pushReg(value, mtI32)
	return nil
}
