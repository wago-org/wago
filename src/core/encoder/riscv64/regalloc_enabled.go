//go:build wago_regalloccheck

package riscv64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = true

type regallocState struct {
	gpWriteObserver  func(uint32)
	fpWriteObserver  func(uint32)
	regallocObserver func(regalloccheck.Effect)
}

// ObserveGPWrites scopes an independent typed physical-write observer. Restore
// the returned observer on every exit, including panic. Bits name X1..X31; X0
// writes are discarded. Any nonzero-link call conservatively reports all GP
// registers except X0, regardless of the guest/host ABI. Raw B writes and fixup
// patching are outside this channel; supported typed emitters report before
// emission, after complete operand/alignment/sticky-error validation.
func (a *Asm) ObserveGPWrites(fn func(uint32)) func(uint32) {
	old := a.gpWriteObserver
	a.gpWriteObserver = fn
	return old
}

// ObserveFPWrites is independent of GP lifetime and symbolic transfer observers.
// Every typed FP definition reports its full physical register before emission;
// a call with a nonzero link register conservatively reports all FP registers.
func (a *Asm) ObserveFPWrites(fn func(uint32)) func(uint32) {
	old := a.fpWriteObserver
	a.fpWriteObserver = fn
	return old
}

func (a *Asm) regallocWrite(dst Reg, call bool) {
	if call && a.fpWriteObserver != nil {
		a.fpWriteObserver(^uint32(0))
	}
	if a.gpWriteObserver == nil {
		return
	}
	mask := uint32(1) << dst
	if call {
		mask = ^uint32(1)
	} else if dst == Zero {
		return
	}
	a.gpWriteObserver(mask)
}

// ObserveRegalloc is an independent transfer-window channel. Current RV64
// compilation does not establish symbolic encoder windows; consumers must supply
// trusted input facts and restore this observer, including on panic. GP lifetime
// observation remains active while this channel is replaced or nested. A Copy
// is reported before its matching GP write; other definitions keep their
// conservative invalidation order. All observations precede byte emission.
func (a *Asm) ObserveRegalloc(fn func(regalloccheck.Effect)) func(regalloccheck.Effect) {
	old := a.regallocObserver
	a.regallocObserver = fn
	return old
}

func gp(reg Reg) regalloccheck.Location { return regalloccheck.Register(regalloccheck.GP, uint8(reg)) }

func (a *Asm) regallocDefinition(dst Reg, call bool) {
	a.regallocWrite(dst, call)
	if a.regallocObserver == nil {
		return
	}
	if call {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Call})
	} else if dst != Zero {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: gp(dst), Size: 8})
	}
}

func (a *Asm) regallocFloatDefinition(dst FReg) {
	if a.fpWriteObserver != nil {
		a.fpWriteObserver(uint32(1) << dst)
	}
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill,
			Dst: regalloccheck.Register(regalloccheck.FP, uint8(dst)), Size: 8})
	}
}

func (a *Asm) regallocImmediate(op, f3 uint32, dst, src Reg, imm int32) {
	call := op == 0x67 && dst != Zero
	if a.regallocObserver == nil {
		a.regallocWrite(dst, call)
		return
	}
	if call {
		a.regallocWrite(dst, true)
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Call})
		return
	}
	if dst == Zero {
		return
	}
	if op == 0x13 && (f3 == 1 || f3 == 5) && imm == 32 && src != Zero {
		// The actual SLLI32/SRLI32 instructions move bytes between halves.
		// Four one-byte copies avoid the vocabulary's GP32-copy high-half
		// invalidation rule, and remain valid when source and destination alias.
		from, to, cleared := uint8(0), uint8(4), uint8(0)
		if f3 == 5 {
			from, to, cleared = 4, 0, 4
		}
		for i := uint8(0); i < 4; i++ {
			d, s := gp(dst), gp(src)
			d.Byte, s.Byte = to+i, from+i
			a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: d, Src: s, Size: 1})
		}
		d := gp(dst)
		d.Byte = cleared
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: d, Size: 4})
		// Keep the physical notification for each of the two instructions in
		// ZeroExtend32. It must not erase a transfer's source before Copy.
		a.regallocWrite(dst, false)
		return
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: gp(dst), Size: 8}
	if op == 0x13 && f3 == 0 && imm == 0 && src != Zero {
		e.Kind, e.Src = regalloccheck.Copy, gp(src)
	} else if op == 0x03 {
		if f3 == 2 || f3 == 6 {
			e.Size, e.ClearTo = 4, 8
		}
		if f3 == 4 {
			e.Size, e.ClearTo = 1, 8
		}
		if src == SP {
			e.Kind, e.Src = regalloccheck.Copy, regalloccheck.Slot(imm)
		}
	}
	if e.Kind == regalloccheck.Copy {
		// Transfer facts precede their matching write notification, so graph
		// observers can preserve self-copies while still seeing every GP write.
		a.regallocObserver(e)
		a.regallocWrite(dst, false)
		return
	}
	// Sign-extending W arithmetic is a semantic definition, never a transfer or
	// proof that high bytes are zero. Kill all eight bytes of its old identity.
	a.regallocWrite(dst, false)
	a.regallocObserver(e)
}

func (a *Asm) regallocStore(src, base Reg, off int32, f3 uint32) {
	if a.regallocObserver == nil || base != SP {
		return
	}
	size := 8
	if f3 == 2 {
		size = 4
	}
	if f3 == 0 {
		size = 1
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Slot(off), Size: size}
	if src != Zero {
		e.Kind, e.Src = regalloccheck.Copy, gp(src)
	}
	a.regallocObserver(e)
}

// LR/SC definitions destroy all prior GP identity, including sign-extended W
// results. A conditional direct-SP store may fail, so conservatively kill its
// frame range rather than report an unconditional Copy. Dynamic addresses remain
// outside this bounded frame-transfer channel.
func (a *Asm) regallocAtomic(dst, base Reg, store bool, size int) {
	a.regallocDefinition(dst, false)
	if store && base == SP && a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Slot(0), Size: size})
	}
}
