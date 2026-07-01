package x64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Linear-memory access: scalar loads/stores with a linear bounds check, plus
// memory.size/grow. Ported from WARP's memory lowering, adapted to wago's runtime
// memory ABI (the same one backend/amd64 targets): the linear-memory base is
// pinned in RBX for the whole function, and a small "basedata" header sits at
// negative offsets from that base.

// Trap codes — must match jit.TrapCode / the values the engine reads (identical
// to backend/amd64's table).
const (
	trapUnreachable   = 1
	trapMemOOB        = 3
	trapDivZero       = 9
	trapDivOverflow   = 10
	trapTruncOverflow = 11
)

// Basedata fields at negative offsets from the linMem base (runtime/basedata.go).
const (
	bdCurPages  = 4  // u32: current size in 64 KiB pages
	bdCurBytes  = 8  // u32: current size in bytes (the bounds-check limit)
	bdMaxPages  = 12 // u32: grow ceiling in pages
	wasmPageLog = 16 // log2(65536)
)

// emitTrap writes the trap code to *trapPtr ([RBP-24]) and returns. It is
// terminal (Leave/Ret), so it may freely clobber RSI even if a value lives there
// — control never returns to the body from here.
func (f *fn) emitTrap(code uint32) {
	f.a.Load64(RSI, RBP, -24)
	f.a.StoreImm32Mem(RSI, 0, int32(code))
	f.a.Leave()
	f.a.Ret()
}

// memAddr pops the address operand, folds the static memarg offset, emits the
// bounds check (unless guard-page mode elides it), and returns the register
// holding the effective offset plus the displacement to fold into the access.
func (f *fn) memAddr(off uint32, size int) (ea Reg, disp int32) {
	ea = f.materialize(f.popValue()) // ea = addr (u32, zero-extended)
	disp = 0
	leaDisp := int32(size)
	if int64(off)+int64(size) <= 0x7FFFFFFF {
		disp = int32(off)
		leaDisp = int32(off) + int32(size)
	} else if off != 0 {
		t := f.allocReg(maskOf(ea))
		f.a.MovImm32(t, int32(off))
		f.a.Add64(ea, t)
		f.release(t)
	}

	if f.guardMode {
		return ea, disp
	}
	f.pinned = f.pinned.add(ea)
	t := f.allocReg(0)
	f.a.LeaDisp(t, ea, leaDisp) // t = ea + off + size
	mb := f.allocReg(maskOf(t))
	f.a.Load32(mb, RBX, -bdCurBytes) // memory size in bytes
	f.a.Cmp64(t, mb)
	ok := f.a.JccPlaceholder(condBE) // in bounds when ea+off+size <= memBytes
	f.emitTrap(trapMemOOB)
	f.a.PatchRel32(ok, f.a.Len())
	f.release(t)
	f.release(mb)
	f.pinned = f.pinned.remove(ea)
	return ea, disp
}

// memLoad lowers a scalar load of `size` bytes. signed selects sign-extension;
// wide selects an i64 result (so signed sub-width loads extend to all 64 bits).
func (f *fn) memLoad(r *wasm.Reader, size int, signed, wide bool) error {
	if _, err := r.U32(); err != nil { // align (unused)
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	ea, disp := f.memAddr(off, size)
	f.a.LoadIdx(ea, RBX, ea, disp, size, signed, wide) // ea = mem[RBX + ea + disp]
	typ := mtI32
	if wide {
		typ = mtI64
	}
	f.pushReg(ea, typ)
	return nil
}

// memStore lowers a scalar store of `size` bytes.
func (f *fn) memStore(r *wasm.Reader, size int) error {
	if _, err := r.U32(); err != nil { // align (unused)
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	vreg := f.materialize(f.popValue())
	f.pinned = f.pinned.add(vreg)
	ea, disp := f.memAddr(off, size)
	f.a.StoreIdx(RBX, ea, vreg, disp, size)
	f.pinned = f.pinned.remove(vreg)
	f.release(ea)
	f.release(vreg)
	return nil
}

// memorySize pushes the current linear-memory size in pages.
func (f *fn) memorySize(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	out := f.allocReg(0)
	f.a.Load32(out, RBX, -bdCurPages)
	f.pushReg(out, mtI32)
	return nil
}

// memoryGrow grows linear memory by the popped page delta, pushing the previous
// size in pages or -1 on failure. The reservation is mapped up front, so this is
// a pure size-cache update (matching backend/amd64); the base never moves.
func (f *fn) memoryGrow(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	res := f.allocReg(maskOf(delta))
	f.a.Load32(res, RBX, -bdCurPages) // old pages — the success result
	nw := f.allocReg(maskOf(delta).add(res))
	f.a.MovRegReg32(nw, res)
	f.a.Add32(nw, delta) // new = old + delta; CF on u32 overflow
	failOverflow := f.a.JccPlaceholder(condB)
	mx := f.allocReg(maskOf(delta).add(res).add(nw))
	f.a.Load32(mx, RBX, -bdMaxPages)
	f.a.Cmp32(nw, mx)
	failMax := f.a.JccPlaceholder(condA) // new > max
	f.a.Store32(RBX, -bdCurPages, nw)
	f.a.MovRegReg32(mx, nw)
	f.a.ShiftImm(4, mx, wasmPageLog, false) // bytes = pages << 16 (digit 4 = shl)
	f.a.Store32(RBX, -bdCurBytes, mx)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(failOverflow, f.a.Len())
	f.a.PatchRel32(failMax, f.a.Len())
	f.a.MovImm32(res, -1)
	f.a.PatchRel32(done, f.a.Len())
	f.pinned = f.pinned.remove(delta)
	f.release(delta)
	f.release(nw)
	f.release(mx)
	f.pushReg(res, mtI32)
	return nil
}
