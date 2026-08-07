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
	case d.Class == railshared.AtomicRMW && d.Operation == railshared.AtomicAdd && d.Size == 4 && d.ResultSize == 4:
		return f.atomicRMWAdd32(d.Offset)
	default:
		return fmt.Errorf("arm64: unsupported 0xFE opcode %d", d.Sub)
	}
}

func (f *fn) atomicRMWAdd32(off uint64) error {
	// Atomics are observable memory barriers: realize deferred reads and discard
	// the scalar store-forwarding window before entering the exclusive loop.
	f.materializePendingLoads()
	f.invalidateStoreForward()

	value := f.popValue()
	vreg, vOwned := f.materializeRead(value)
	f.pinned = f.pinned.add(vreg)
	base := linMemReg
	var ea Reg
	var eaOwned bool
	var disp int32
	if f.threadedMemory0 {
		base, ea, disp = f.indexedMemAddr(0, off, 4)
		eaOwned = true
	} else {
		ea, eaOwned, _, disp = f.memAddr(off, 4, true)
	}

	addr := f.allocReg(maskOf(vreg, base, ea))
	f.a.Add64(addr, base, ea)
	f.addDisp(addr, addr, disp, true)
	if f.threadedMemory0 {
		f.release(base)
	}
	if eaOwned {
		f.release(ea)
	}
	if !f.a.TstImm64(addr, 3) {
		panic("arm64: atomic alignment mask is not encodable")
	}
	f.trapIf(condNE, trapAtomicUnaligned)

	old := f.allocReg(maskOf(vreg, addr))
	next := f.allocReg(maskOf(vreg, addr, old))
	status := f.allocReg(maskOf(vreg, addr, old, next))
	loop := f.a.Len()
	f.a.Ldaxr32(old, addr)
	f.a.Add32(next, old, vreg)
	f.a.Stlxr32(status, next, addr)
	f.a.PatchBranch19(f.a.Cbnz64(status), loop)

	f.release(status)
	f.release(next)
	f.release(addr)
	f.pinned = f.pinned.remove(vreg)
	if vOwned {
		f.release(vreg)
	}
	f.pushReg(old, mtI32)
	return nil
}
