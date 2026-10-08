//go:build wago_regalloccheck

package amd64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = true

type regallocState struct {
	regallocObserver func(regalloccheck.Effect)
	gpWriteObserver  func(uint32)
	fpWriteObserver  func(uint32)
}

// ObserveGPWrites installs a physical GP destination observer independently of
// transfer windows. Bit n names register n, including RSP and RBP. Partial and
// conditional writes count; calls conservatively report all sixteen registers.
// Restore the returned observer when the scope ends, including on panic.
func (a *Asm) ObserveGPWrites(fn func(uint32)) func(uint32) {
	old := a.gpWriteObserver
	a.gpWriteObserver = fn
	return old
}

func (a *Asm) regallocGPWrite(mask uint32) {
	if mask != 0 && a.gpWriteObserver != nil {
		a.gpWriteObserver(mask)
	}
}

// GP and FP ModRM destinations use the same physical extension-bit rules.
func regallocGPRegMask(reg Reg) uint32 { return regallocFPRegMask(reg) }

// The one-byte register ALU family uses either ModRM destination direction.
// CMP and TEST only update flags. MOV and XCHG also use this shared seam.
func (a *Asm) regallocGPRR(op byte, rm, reg Reg, rex bool) {
	if !rex && op&1 == 0 && (op <= 0x33 || op >= 0x86 && op <= 0x8a) {
		rm, reg = regallocLegacyByteReg(rm), regallocLegacyByteReg(reg)
	}
	switch {
	case op <= 0x33 && op&7 <= 3:
		if op&2 == 0 {
			a.regallocGPWrite(regallocGPRegMask(rm))
		} else {
			a.regallocGPWrite(regallocGPRegMask(reg))
		}
	case op == 0x88 || op == 0x89:
		a.regallocGPWrite(regallocGPRegMask(rm))
	case op == 0x8a || op == 0x8b:
		a.regallocGPWrite(regallocGPRegMask(reg))
	case op == 0x86 || op == 0x87:
		a.regallocGPWrite(regallocGPRegMask(rm) | regallocGPRegMask(reg))
	case op >= 0x38 && op <= 0x3b || op == 0x84 || op == 0x85: // CMP, TEST
	case op == 0xff:
		switch reg & 7 {
		case 0, 1: // INC/DEC r/m
			a.regallocGPWrite(regallocGPRegMask(rm))
		case 2, 3: // CALL r/m
			a.regallocGPWrite(0xffff)
			regallocCall(a)
		case 4, 5: // JMP
		case 6: // PUSH r/m
			a.regallocGPWrite(1 << RSP)
		default:
			a.regallocGPWrite(0xffff)
			a.regallocFPWrite(0xffff)
		}
	default:
		a.regallocGPWrite(0xffff)
		a.regallocFPWrite(0xffff)
	}
}

// Without REX, byte encodings 4..7 name AH/CH/DH/BH, not SPL/BPL/SIL/DIL.
func regallocLegacyByteReg(r Reg) Reg {
	if r >= 4 && r < 8 {
		return r - 4
	}
	return r
}

// Classify GP effects of generic one-byte memory forms, including XCHG and
// group-5 implicit writes. Unknown forms cannot establish GP preservation.
func (a *Asm) regallocGPMem(op byte, reg Reg, rex bool) {
	if !rex && op&1 == 0 && (op <= 0x33 || op == 0x86 || op == 0x8a) {
		reg = regallocLegacyByteReg(reg)
	}
	switch {
	case op <= 0x33 && op&6 == 2:
		a.regallocGPWrite(regallocGPRegMask(reg))
	case op == 0x63 || op == 0x8a || op == 0x8b || op == 0x8d:
		a.regallocGPWrite(regallocGPRegMask(reg))
	case op == 0x86 || op == 0x87: // XCHG also writes its register operand
		a.regallocGPWrite(regallocGPRegMask(reg))
	case op <= 0x33 && op&7 <= 1: // ALU memory destination
	case op >= 0x38 && op <= 0x3b || op == 0x84 || op == 0x85 || op == 0x88 || op == 0x89:
		// CMP, TEST, MOV to memory
	case op == 0xff:
		switch reg & 7 {
		case 0, 1, 4, 5: // INC/DEC memory or JMP
		case 2, 3: // CALL r/m
			a.regallocGPWrite(0xffff)
			regallocCall(a)
		case 6: // PUSH r/m
			a.regallocGPWrite(1 << RSP)
		default:
			a.regallocGPWrite(0xffff)
			a.regallocFPWrite(0xffff)
		}
	default:
		a.regallocGPWrite(0xffff)
		a.regallocFPWrite(0xffff)
	}
}

// Observe the emitted call independently of backend ABI/call-presence hints.
// Call invalidates both register banks; caller-frame effects require separate
// contracts for argument/result slots and changes to the frame origin.
func regallocCall(a *Asm) {
	a.regallocFPWrite(0xffff)
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Call})
	}
}

// Known scalar/packed arithmetic destroys its result bytes independently of the backend's
// semantic definition. Legacy SSE preserves the destination's upper lanes;
// VEX scalar instructions copy those lanes from their first source. This seam
// deliberately admits only the arithmetic/round/conversion opcodes below.
// Moves retain their dedicated transfer effects and must not be killed twice.
func regallocScalarFP(a *Asm, prefix, opcodeMap, op byte, dst, left Reg, vex bool) {
	if a.regallocObserver == nil {
		return
	}
	if prefix != 0 && prefix != 0x66 && prefix != 0xf2 && prefix != 0xf3 {
		return
	}
	size := 16
	if prefix == 0xf2 {
		size = 8
	} else if prefix == 0xf3 {
		size = 4
	}
	switch opcodeMap {
	case 0:
		switch op {
		case 0x51, 0x58, 0x59, 0x5c, 0x5d, 0x5e, 0x5f, 0xc2:
		case 0x2a: // scalar integer-to-float
			if size == 16 {
				return // packed/MMX forms are outside this seam
			}
		case 0x5a: // precision conversion: prefix names the input precision
			if prefix == 0xf2 {
				size = 4
			} else if prefix == 0xf3 {
				size = 8
			}
		default:
			return
		}
	case 0x3a:
		if prefix != 0x66 {
			return
		}
		switch op {
		case 0x08, 0x09: // ROUNDPS/PD
			size = 16
		case 0x0a: // ROUNDSS
			size = 4
		case 0x0b: // ROUNDSD
			size = 8
		default:
			return
		}
	default:
		return
	}
	// Match the actual ModRM.reg field and REX/VEX.R selection. The raw
	// encoder treats every Reg >= 8 as an extension-bit request, even if the
	// caller supplied an out-of-range Reg rather than a canonical physical ID.
	encodedDst := uint8(dst & 7)
	if dst >= 8 {
		encodedDst |= 8
	}
	d := regalloccheck.Register(regalloccheck.FP, encodedDst)
	if vex && size < 16 {
		upperDst, upperSrc := d, regalloccheck.Register(regalloccheck.FP, uint8(left&15))
		upperDst.Byte, upperSrc.Byte = uint8(size), uint8(size)
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: upperDst, Src: upperSrc, Size: 16 - size})
	}
	a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: d, Size: size})
}

func regallocVexScalarFP(a *Asm, opcodeMap, pp, op byte, dst, left Reg, l byte) {
	var m byte
	switch opcodeMap {
	case vexMap0F:
	case vexMap0F3A:
		m = 0x3a
	default:
		return
	}
	// Scalar L=1 forms are outside this known-form contract. Their absence
	// must remain unsupported in a whole-body journal; it is not evidence of
	// preserved bytes. Packed L=1 writes kill all tracked (low 16) bytes.
	if l != 0 && (m == 0 && pp&3 >= 2 || m == 0x3a && (op == 0x0a || op == 0x0b)) {
		return
	}
	regallocScalarFP(a, [...]byte{0, 0x66, 0xf3, 0xf2}[pp&3], m, op, dst, left, true)
}

// GP results hidden in the generic SIMD encoders need the actual ModRM
// destination, not the helper's argument names. Memory extraction writes only
// memory. Mandatory prefixes distinguish scalar GP conversions from packed FP
// conversions and MOVD/Q from the FP-only F3 MOVQ form.
func (a *Asm) regallocGPSSE(prefix, opcodeMap, op byte, reg, rm Reg, memory bool) {
	if regallocFPSSEMask(prefix, opcodeMap, op, reg, rm, memory) == 0xffff {
		a.regallocGPWrite(0xffff)
		return
	}
	if opcodeMap == 0 {
		switch op {
		case 0x2c, 0x2d: // CVTTSS/SD2SI, CVTSS/SD2SI
			if prefix == 0xf2 || prefix == 0xf3 {
				a.regallocGPWrite(regallocGPRegMask(reg))
			}
		case 0x50, 0xd7, 0xc5: // MOVMSKPS/PD, PMOVMSKB, PEXTRW
			if !memory && (prefix == 0 || prefix == 0x66) {
				a.regallocGPWrite(regallocGPRegMask(reg))
			}
		case 0x7e: // MOVD/Q r/m, xmm (or mm)
			if !memory && (prefix == 0 || prefix == 0x66) {
				a.regallocGPWrite(regallocGPRegMask(rm))
			}
		}
	} else if opcodeMap == 0x3a && prefix == 0x66 && !memory {
		switch op {
		case 0x14, 0x15, 0x16, 0x17: // PEXTRB/W/D/Q, EXTRACTPS
			a.regallocGPWrite(regallocGPRegMask(rm))
		}
	}
}

func (a *Asm) regallocGPVEX(opcodeMap, pp, op byte, reg, rm Reg, memory bool) {
	if pp&^3 != 0 {
		a.regallocGPWrite(0xffff)
		return
	}
	if opcodeMap == vexMap0F3A && pp == 3 && op == 0xf0 { // RORX
		a.regallocGPWrite(regallocGPRegMask(reg))
		return
	}
	var m byte
	switch opcodeMap {
	case vexMap0F:
	case vexMap0F38:
		m = 0x38
	case vexMap0F3A:
		m = 0x3a
	default:
		a.regallocGPWrite(0xffff)
		return
	}
	prefix := [...]byte{0, 0x66, 0xf3, 0xf2}[pp&3]
	a.regallocGPSSE(prefix, m, op, reg, rm, memory)
}

// ObserveRegalloc scopes an observer to an explicitly checked transfer window.
// The returned observer must be restored, including when codegen panics.
func (a *Asm) ObserveRegalloc(fn func(regalloccheck.Effect)) func(regalloccheck.Effect) {
	old := a.regallocObserver
	a.regallocObserver = fn
	return old
}
func regallocBank(fp bool) regalloccheck.Bank {
	if fp {
		return regalloccheck.FP
	}
	return regalloccheck.GP
}

// Legacy scalar FP register moves preserve their untouched upper lanes;
// scalar memory loads and GP-to-FP moves do not.
func (a *Asm) regallocCopy(dst, src Reg, fp bool, size int) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
}
func (a *Asm) regallocSwap(dst, src Reg, size int) {
	if a.regallocObserver != nil {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Swap, Dst: regalloccheck.Register(regalloccheck.GP, uint8(dst)), Src: regalloccheck.Register(regalloccheck.GP, uint8(src)), Size: size})
	}
}

// Only direct stack-pointer-relative memory names tracked frame bytes. Other
// loads destroy the old destination identity; pointer aliases and changing frame
// bases are outside this model. Scalar FP loads also kill old upper lanes.
func (a *Asm) regallocLoad(dst, base Reg, offset int32, fp bool, size int) {
	if a.regallocObserver == nil {
		return
	}
	e := regalloccheck.Effect{Kind: regalloccheck.Kill, Dst: regalloccheck.Register(regallocBank(fp), uint8(dst)), Size: size}
	if base == RSP {
		e.Kind = regalloccheck.Copy
		e.Src = regalloccheck.Slot(offset)
	}
	if fp {
		e.ClearTo = 16
	} else if size == 4 {
		e.ClearTo = 8
	}
	a.regallocObserver(e)
}

// Non-frame stores cannot establish or update facts about the tracked frame.
func (a *Asm) regallocStore(base Reg, offset int32, src Reg, fp bool, size int) {
	if a.regallocObserver != nil && base == RSP {
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Slot(offset), Src: regalloccheck.Register(regallocBank(fp), uint8(src)), Size: size})
	}
}
func regallocWidth(wide bool) int {
	if wide {
		return 8
	}
	return 4
}

func (a *Asm) regallocRead(base Reg, offset int32, size int) {
	if a.regallocObserver != nil {
		src := regalloccheck.Location{Bank: regalloccheck.Unknown} // non-frame memory cannot prove a frame input
		if base == RSP {
			src = regalloccheck.Slot(offset)
		}
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Read, Src: src, Size: size})
	}
}

// A cross-bank scalar move defines only the copied low bytes. GP32 and scalar
// FP writes clear their high carrier bytes on both targets.
func (a *Asm) regallocCrossCopy(dst, src Reg, dstFP bool, size int) {
	if a.regallocObserver == nil {
		return
	}
	clearTo := 8
	if dstFP {
		clearTo = 16
	}
	a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Copy,
		Dst:  regalloccheck.Register(regallocBank(dstFP), uint8(dst)),
		Src:  regalloccheck.Register(regallocBank(!dstFP), uint8(src)),
		Size: size, ClearTo: clearTo})
}

// An untracked FP definition invalidates the previous vector identity. The
// backend installs a semantic result identity when its contract permits one.
func (a *Asm) regallocKillFP(dst Reg) {
	if a.regallocObserver != nil {
		// Match the ModRM register field and the encoder's extension-bit rule.
		encodedDst := uint8(dst & 7)
		if dst >= 8 {
			encodedDst |= 8
		}
		a.regallocObserver(regalloccheck.Effect{Kind: regalloccheck.Kill,
			Dst: regalloccheck.Register(regalloccheck.FP, encodedDst), Size: 16})
	}
}
