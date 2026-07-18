package riscv64

// VSEW is the selected vector element width for vsetivli. The encoder targets
// LMUL=1 and exact 128-bit WebAssembly vectors; callers select the active lane
// count needed for the operation (16/8/4/2 for E8/E16/E32/E64).
type VSEW uint8

const (
	VE8 VSEW = iota
	VE16
	VE32
	VE64
)

// Vsetivli sets an immediate AVL with LMUL=1, tail-agnostic and mask-agnostic
// policy. WebAssembly v128 lowering uses a complete 128-bit active prefix, so
// inactive implementation-specific tail elements are never observed or stored.
func (a *Asm) Vsetivli(rd Reg, avl uint8, sew VSEW) bool {
	if avl > 31 || sew > VE64 {
		return false
	}
	// zimm10 is vtype[9:0]: vill=0, vma=1, vta=1, vsew, vlmul=m1(0).
	vtype := uint32(0xc0 | uint8(sew)<<3)
	a.word(0xc0007057 | vtype<<20 | uint32(avl)<<15 | r(rd)<<7)
	return true
}

func vectorWidthFunct3(sew VSEW) (uint32, bool) {
	switch sew {
	case VE8:
		return 0, true
	case VE16:
		return 5, true
	case VE32:
		return 6, true
	case VE64:
		return 7, true
	default:
		return 0, false
	}
}

// Vle emits an unmasked unit-stride vector load with nf=0 and lumop=0.
func (a *Asm) Vle(vd, base Reg, sew VSEW) bool {
	width, ok := vectorWidthFunct3(sew)
	if !ok {
		return false
	}
	a.word(0x02000007 | width<<12 | r(base)<<15 | r(vd)<<7)
	return true
}

// Vse emits an unmasked unit-stride vector store with nf=0 and sumop=0.
func (a *Asm) Vse(vs3, base Reg, sew VSEW) bool {
	width, ok := vectorWidthFunct3(sew)
	if !ok {
		return false
	}
	a.word(0x02000027 | width<<12 | r(base)<<15 | r(vs3)<<7)
	return true
}

func (a *Asm) vop(funct6, funct3 uint32, vd, vs2, src1 Reg, vm bool) {
	mask := uint32(0)
	if vm {
		mask = 1 << 25
	}
	a.word(0x57 | r(vd)<<7 | (funct3&7)<<12 | r(src1)<<15 | r(vs2)<<20 | mask | (funct6&0x3f)<<26)
}

// Integer vector-vector operations (OPIVV).
func (a *Asm) VaddVV(vd, vs2, vs1 Reg)     { a.vop(0x00, 0, vd, vs2, vs1, true) }
func (a *Asm) VsubVV(vd, vs2, vs1 Reg)     { a.vop(0x02, 0, vd, vs2, vs1, true) }
func (a *Asm) VminuVV(vd, vs2, vs1 Reg)    { a.vop(0x04, 0, vd, vs2, vs1, true) }
func (a *Asm) VminVV(vd, vs2, vs1 Reg)     { a.vop(0x05, 0, vd, vs2, vs1, true) }
func (a *Asm) VmaxuVV(vd, vs2, vs1 Reg)    { a.vop(0x06, 0, vd, vs2, vs1, true) }
func (a *Asm) VmaxVV(vd, vs2, vs1 Reg)     { a.vop(0x07, 0, vd, vs2, vs1, true) }
func (a *Asm) VandVV(vd, vs2, vs1 Reg)     { a.vop(0x09, 0, vd, vs2, vs1, true) }
func (a *Asm) VorVV(vd, vs2, vs1 Reg)      { a.vop(0x0a, 0, vd, vs2, vs1, true) }
func (a *Asm) VxorVV(vd, vs2, vs1 Reg)     { a.vop(0x0b, 0, vd, vs2, vs1, true) }
func (a *Asm) VrgatherVV(vd, vs2, vs1 Reg) { a.vop(0x0c, 0, vd, vs2, vs1, true) }
func (a *Asm) VmulVV(vd, vs2, vs1 Reg)     { a.vop(0x25, 2, vd, vs2, vs1, true) }

// VmvVV copies an entire active vector register.
func (a *Asm) VmvVV(vd, vs1 Reg) { a.vop(0x17, 0, vd, Zero, vs1, true) }

// Floating vector-vector operations (OPFVV); SEW selects f32/f64 lanes through
// the current vtype.
func (a *Asm) VfaddVV(vd, vs2, vs1 Reg)   { a.vop(0x00, 1, vd, vs2, vs1, true) }
func (a *Asm) VfsubVV(vd, vs2, vs1 Reg)   { a.vop(0x02, 1, vd, vs2, vs1, true) }
func (a *Asm) VfminVV(vd, vs2, vs1 Reg)   { a.vop(0x04, 1, vd, vs2, vs1, true) }
func (a *Asm) VfmaxVV(vd, vs2, vs1 Reg)   { a.vop(0x06, 1, vd, vs2, vs1, true) }
func (a *Asm) VfsgnjVV(vd, vs2, vs1 Reg)  { a.vop(0x08, 1, vd, vs2, vs1, true) }
func (a *Asm) VfsgnjnVV(vd, vs2, vs1 Reg) { a.vop(0x09, 1, vd, vs2, vs1, true) }
func (a *Asm) VfsgnjxVV(vd, vs2, vs1 Reg) { a.vop(0x0a, 1, vd, vs2, vs1, true) }
func (a *Asm) VfdivVV(vd, vs2, vs1 Reg)   { a.vop(0x20, 1, vd, vs2, vs1, true) }
func (a *Asm) VfmulVV(vd, vs2, vs1 Reg)   { a.vop(0x24, 1, vd, vs2, vs1, true) }

func (a *Asm) VfsqrtV(vd, vs2 Reg) { a.vop(0x13, 1, vd, vs2, Zero, true) }
