//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) emitFE(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 0x1e: // i32.atomic.rmw.add
		return f.atomicRMWAdd32(r)
	default:
		return fmt.Errorf("amd64: unsupported 0xFE opcode %d", sub)
	}
}

func (f *fn) atomicRMWAdd32(r *wasm.Reader) error {
	_, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	f.invalidateStoreForward()
	value := f.materialize(f.popValue())
	f.pinned = f.pinned.add(value)
	base, ea, disp := f.indexedMemAddr(0, off, 4)
	f.a.LockXaddIdx32(base, ea, value, disp)
	f.release(base)
	f.release(ea)
	f.pinned = f.pinned.remove(value)
	f.pushReg(value, mtI32)
	return nil
}
