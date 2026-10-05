//go:build wago_regalloccheck

package riscv64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = true

type regallocState struct {
	gpWriteObserver  func(uint32)
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

func (a *Asm) regallocWrite(dst Reg, call bool) {
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
// observation remains active while this channel is replaced or nested.
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
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill,
			Dst: regalloccheck.Register(regalloccheck.FP, uint8(dst)), Size: 8})
	}
}

func (a *Asm) regallocImmediate(op, f3 uint32, dst, src Reg, imm int32) {
	call := op == 0x67 && dst != Zero
	a.regallocWrite(dst, call)
	if a.regallocObserver == nil {
		return
	}
	if call {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Call})
		return
	}
	if dst == Zero {
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
	// Sign-extending W arithmetic is a semantic definition, never a transfer or
	// proof that high bytes are zero. Kill all eight bytes of its old identity.
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
