package arm64

// Minimal AArch64 wasm→native compiler — the P2 beachhead. It lowers integer
// functions (locals, i32 arithmetic/bitwise/shift/compare, and structured
// control flow: block/loop/if/else/br/br_if) to AArch64 via the arm64 encoder,
// proving the wasm→arm64 codegen path end-to-end before the full railshot
// architecture (valent-block operand stack, hint-driven pinning, the shared
// neutral core) is wired in. It is intentionally naive: each local gets a
// dedicated callee-saved register, every local.get copies to a fresh scratch, and
// only void block types are supported. It grows into the real backend as the
// neutral core is extracted from railshot/amd64; treat the codegen quality as
// throwaway.
//
// Calling convention (beachhead): up to 8 i32 params arrive in X0..X7 (AAPCS64)
// and are moved into their local registers in the prologue; the single result
// leaves in X0. Locals occupy X19..X27 (callee-saved; safe in a leaf, and the
// trampoline preserves them). Temporaries use the X9..X15 scratch pool.

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

var scratchRegs = []a64.Reg{a64.X9, a64.X10, a64.X11, a64.X12, a64.X13, a64.X14, a64.X15}

// localRegs are the callee-saved registers holding wasm locals (index 0 = X19).
var localRegs = []a64.Reg{a64.X19, a64.X20, a64.X21, a64.X22, a64.X23, a64.X24, a64.X25, a64.X26, a64.X27}

type opnd struct {
	isConst bool
	reg     a64.Reg
	cval    int64
}

// pend is a forward branch site awaiting a patch to its block's end.
type pend struct {
	at   int
	wide bool // true = imm26 (B), false = imm19 (B.cond / CBZ / CBNZ)
}

type frame struct {
	isFunc bool
	isLoop bool
	isIf   bool
	header int    // loop: back-branch target offset
	else_  int    // if: the CBZ site to else/end (-1 once patched)
	pend   []pend // forward branches to patch at this block's end
}

type comp struct {
	a        a64.Asm
	stack    []opnd
	free     []a64.Reg
	localReg []a64.Reg
	ctrl     []*frame
}

// Compile lowers a single function body (local-decl vector + instruction stream)
// with numParams i32 params to AArch64 machine code.
func Compile(numParams int, body []byte) ([]byte, error) {
	c := &comp{free: append([]a64.Reg(nil), scratchRegs...)}
	r := wasm.NewReader(body)

	// Parse the local-declaration vector to size the local register file.
	nGroups, err := r.U32()
	if err != nil {
		return nil, fmt.Errorf("local decls: %w", err)
	}
	declared := 0
	for i := uint32(0); i < nGroups; i++ {
		n, err := r.U32()
		if err != nil {
			return nil, err
		}
		if _, err := r.Byte(); err != nil { // value type (ignored; treated as int)
			return nil, err
		}
		declared += int(n)
	}
	total := numParams + declared
	if total > len(localRegs) {
		return nil, fmt.Errorf("beachhead supports up to %d locals, got %d", len(localRegs), total)
	}
	for i := 0; i < total; i++ {
		c.localReg = append(c.localReg, localRegs[i])
	}

	// Prologue: move params into their local registers, zero declared locals.
	for i := 0; i < numParams; i++ {
		c.a.MovReg32(c.localReg[i], a64.X0+a64.Reg(i))
	}
	for i := numParams; i < total; i++ {
		c.a.MovImm64(c.localReg[i], 0)
	}

	c.ctrl = []*frame{{isFunc: true}}

	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x02: // block
			if err := c.blockType(r); err != nil {
				return nil, err
			}
			c.ctrl = append(c.ctrl, &frame{else_: -1})
		case 0x03: // loop
			if err := c.blockType(r); err != nil {
				return nil, err
			}
			c.ctrl = append(c.ctrl, &frame{isLoop: true, header: c.a.Len(), else_: -1})
		case 0x04: // if
			if err := c.blockType(r); err != nil {
				return nil, err
			}
			rc := c.materialize(c.pop())
			site := c.a.Cbz64(rc) // fall into the else/end when the condition is false
			c.freeIfScratch(rc)
			c.ctrl = append(c.ctrl, &frame{isIf: true, else_: site})
		case 0x05: // else
			fr := c.ctrl[len(c.ctrl)-1]
			at := c.a.Branch() // end of the true arm: jump over the else body
			fr.pend = append(fr.pend, pend{at, true})
			c.a.PatchBranch19(fr.else_, c.a.Len()) // false path lands here (else body)
			fr.else_ = -1
		case 0x0b: // end
			fr := c.ctrl[len(c.ctrl)-1]
			c.ctrl = c.ctrl[:len(c.ctrl)-1]
			if fr.isFunc {
				c.emitReturn()
				return c.a.B, nil
			}
			here := c.a.Len()
			for _, p := range fr.pend {
				if p.wide {
					c.a.PatchBranch26(p.at, here)
				} else {
					c.a.PatchBranch19(p.at, here)
				}
			}
			if fr.isIf && fr.else_ >= 0 {
				c.a.PatchBranch19(fr.else_, here) // no else: false path skips the arm
			}
		case 0x0c: // br
			if err := c.br(r); err != nil {
				return nil, err
			}
		case 0x0d: // br_if
			if err := c.brIf(r); err != nil {
				return nil, err
			}
		case 0x0f: // return
			c.emitReturn()
		case 0x20: // local.get
			idx, err := r.U32()
			if err != nil {
				return nil, err
			}
			rd := c.alloc()
			c.a.MovReg32(rd, c.localReg[idx]) // copy so a later local.set can't alias
			c.push(opnd{reg: rd})
		case 0x21: // local.set
			idx, err := r.U32()
			if err != nil {
				return nil, err
			}
			ro := c.materialize(c.pop())
			c.a.MovReg32(c.localReg[idx], ro)
			c.freeIfScratch(ro)
		case 0x22: // local.tee
			idx, err := r.U32()
			if err != nil {
				return nil, err
			}
			ro := c.materialize(c.pop())
			c.a.MovReg32(c.localReg[idx], ro)
			c.push(opnd{reg: ro}) // leaves the value on the stack (already a copy)
		case 0x41: // i32.const
			v, err := r.I32()
			if err != nil {
				return nil, err
			}
			c.push(opnd{isConst: true, cval: int64(v)})
		case 0x45: // i32.eqz
			c.eqz()
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f: // i32 compares
			c.compare(op)
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76: // i32 arith/bitwise/shift
			c.binop(op)
		default:
			return nil, fmt.Errorf("unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("function body did not terminate with end (0x0b)")
}

// blockType reads and validates a block type: only void (0x40) is supported.
func (c *comp) blockType(r *wasm.Reader) error {
	bt, err := r.Byte()
	if err != nil {
		return err
	}
	if bt != 0x40 {
		return fmt.Errorf("beachhead supports only void block type, got %#x", bt)
	}
	return nil
}

// emitReturn moves the top operand (if any) to X0 and returns.
func (c *comp) emitReturn() {
	if len(c.stack) > 0 {
		res := c.materialize(c.pop())
		if res != a64.X0 {
			c.a.MovReg32(a64.X0, res)
		}
	}
	c.a.Ret()
}

func (c *comp) br(r *wasm.Reader) error {
	l, err := r.U32()
	if err != nil {
		return err
	}
	fr := c.ctrl[len(c.ctrl)-1-int(l)]
	switch {
	case fr.isFunc:
		c.emitReturn()
	case fr.isLoop:
		at := c.a.Branch()
		c.a.PatchBranch26(at, fr.header)
	default:
		fr.pend = append(fr.pend, pend{c.a.Branch(), true})
	}
	return nil
}

func (c *comp) brIf(r *wasm.Reader) error {
	l, err := r.U32()
	if err != nil {
		return err
	}
	rc := c.materialize(c.pop())
	fr := c.ctrl[len(c.ctrl)-1-int(l)]
	switch {
	case fr.isLoop:
		c.a.PatchBranch19(c.a.Cbnz64(rc), fr.header)
	case fr.isFunc:
		// Conditional return: skip the return when the condition is false.
		skip := c.a.Cbz64(rc)
		c.emitReturn()
		c.a.PatchBranch19(skip, c.a.Len())
	default:
		fr.pend = append(fr.pend, pend{c.a.Cbnz64(rc), false})
	}
	c.freeIfScratch(rc)
	return nil
}

func (c *comp) push(o opnd) { c.stack = append(c.stack, o) }

func (c *comp) pop() opnd {
	o := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	return o
}

// materialize returns a register holding o's value, allocating a scratch for a
// constant (zero-extending the i32 into a 64-bit register).
func (c *comp) materialize(o opnd) a64.Reg {
	if o.isConst {
		rd := c.alloc()
		c.a.MovImm64(rd, uint64(uint32(o.cval)))
		return rd
	}
	return o.reg
}

func (c *comp) binop(op byte) {
	b := c.pop()
	a := c.pop()
	ra := c.materialize(a)
	rb := c.materialize(b)
	rd := c.alloc()
	switch op {
	case 0x6a:
		c.a.Add32(rd, ra, rb)
	case 0x6b:
		c.a.Sub32(rd, ra, rb)
	case 0x6c:
		c.a.Mul32(rd, ra, rb)
	case 0x71:
		c.a.And32(rd, ra, rb)
	case 0x72:
		c.a.Orr32(rd, ra, rb)
	case 0x73:
		c.a.Eor32(rd, ra, rb)
	case 0x74:
		c.a.Lslv32(rd, ra, rb)
	case 0x75:
		c.a.Asrv32(rd, ra, rb) // shr_s
	case 0x76:
		c.a.Lsrv32(rd, ra, rb) // shr_u
	}
	c.freeIfScratch(ra)
	c.freeIfScratch(rb)
	c.push(opnd{reg: rd})
}

func condFor(op byte) a64.Cond {
	switch op {
	case 0x46:
		return a64.CondEQ
	case 0x47:
		return a64.CondNE
	case 0x48:
		return a64.CondLT
	case 0x49:
		return a64.CondCC
	case 0x4a:
		return a64.CondGT
	case 0x4b:
		return a64.CondHI
	case 0x4c:
		return a64.CondLE
	case 0x4d:
		return a64.CondLS
	case 0x4e:
		return a64.CondGE
	case 0x4f:
		return a64.CondCS
	}
	panic("condFor: not a compare opcode")
}

func (c *comp) compare(op byte) {
	b := c.pop()
	a := c.pop()
	ra := c.materialize(a)
	rb := c.materialize(b)
	c.a.CmpReg32(ra, rb)
	rd := c.alloc()
	c.a.Cset32(rd, condFor(op))
	c.freeIfScratch(ra)
	c.freeIfScratch(rb)
	c.push(opnd{reg: rd})
}

func (c *comp) eqz() {
	ra := c.materialize(c.pop())
	c.a.CmpImm32(ra, 0)
	rd := c.alloc()
	c.a.Cset32(rd, a64.CondEQ)
	c.freeIfScratch(ra)
	c.push(opnd{reg: rd})
}

func (c *comp) alloc() a64.Reg {
	if len(c.free) == 0 {
		panic("arm64 beachhead: out of scratch registers (expression too deep)")
	}
	r := c.free[0]
	c.free = c.free[1:]
	return r
}

func (c *comp) freeIfScratch(r a64.Reg) {
	for _, s := range scratchRegs {
		if s == r {
			c.free = append(c.free, r)
			return
		}
	}
}
