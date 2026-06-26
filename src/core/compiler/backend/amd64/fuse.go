package amd64

import (
	"fmt"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
)

// Comparison fusion.
//
// A WebAssembly comparison (or i32/i64.eqz) normally materializes a 0/1 value
// with setcc. When the very next opcode consumes that boolean directly — an
// `if`, `br_if`, or `select` — the setcc and the consumer's own test/compare
// are both redundant: the branch can read the comparison's EFLAGS straight
// away. This is WARP's "condense comparison" optimization (see
// warp/docs/implementation_details/CondenseComparison.md).
//
// We fuse only when the consumer opcode immediately follows the comparison in
// the bytecode. That keeps EFLAGS live for the span of a single compile step,
// so no intervening instruction can clobber it and there is no deferred state
// to track. Any non-adjacent use falls back to the plain setcc form, which is
// always correct. flush() emits only moves, so it preserves the flags between
// the compare and the branch.

// cmpFused emits an integer comparison, fusing it into an immediately
// following if / br_if / select when present; otherwise it falls back to the
// plain setcc form (g.cmp).
func (g *cg) cmpFused(r *wasm.Reader, cond Cond, w bool) error {
	if n := len(g.st); n >= 2 && bothConst(g.st[n-2], g.st[n-1]) {
		b := g.pop()
		a := g.pop()
		g.push(ventry{kind: vConst, cval: foldCmp(cond, a.cval, b.cval, w)}) // i32 0/1
		return nil
	}
	if next, ok := r.Peek(); ok {
		switch next {
		case 0x04: // if
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitCompare(w))
			return g.fusedIf(r, cond)
		case 0x0D: // br_if
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitCompare(w))
			return g.fusedBrIf(r, cond)
		case 0x1B: // select
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitCompare(w))
			g.fusedSelect(cond, false, false)
			return nil
		}
	}
	g.cmp(cond, w)
	return nil
}

// eqzFused is cmpFused for i32/i64.eqz, whose true condition is CondE (== 0).
func (g *cg) eqzFused(r *wasm.Reader, w bool) error {
	if n := len(g.st); n >= 1 && g.st[n-1].kind == vConst && !g.st[n-1].fp {
		a := g.pop()
		v := int64(0)
		if uw(a.cval, w) == 0 {
			v = 1
		}
		g.push(ventry{kind: vConst, cval: v}) // i32 0/1
		return nil
	}
	if next, ok := r.Peek(); ok {
		switch next {
		case 0x04: // if
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitEqzTest(w))
			return g.fusedIf(r, CondE)
		case 0x0D: // br_if
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitEqzTest(w))
			return g.fusedBrIf(r, CondE)
		case 0x1B: // select
			if _, err := r.Byte(); err != nil { // consume the peeked consumer opcode
				return err
			}
			g.freeReg(g.emitEqzTest(w))
			g.fusedSelect(CondE, false, false)
			return nil
		}
	}
	g.eqz(w)
	return nil
}

// fusedIf opens an `if` frame, jumping to the else/end edge when cond is false.
// Mirrors opBlock's reachable if-path with the condition taken from EFLAGS
// instead of a materialized register.
func (g *cg) fusedIf(r *wasm.Reader, cond Cond) error {
	pN, rN, err := g.blockType(r)
	if err != nil {
		return err
	}
	f := cframe{kind: ckIf, paramN: pN, resultN: rN, branchN: rN, elseSite: -1}
	f.height = len(g.st) - pN
	g.flush()
	f.elseSite = g.a.JccPlaceholder(invertCond(cond)) // skip then-edge when cond false
	g.ctrl = append(g.ctrl, f)
	return nil
}

// fusedBrIf takes the branch when cond is true, reading EFLAGS directly.
// Mirrors opBr's conditional path without the test of a materialized register.
func (g *cg) fusedBrIf(r *wasm.Reader, cond Cond) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(g.ctrl) - 1 - int(idx)
	if fi < 0 {
		return fmt.Errorf("br label out of range")
	}
	f := &g.ctrl[fi]
	a, base, d := f.branchN, f.height, len(g.st)
	g.flush()
	over := g.a.JccPlaceholder(invertCond(cond)) // skip the branch when cond false
	g.moveSlots(d-a, base, a)
	g.branchJump(f)
	g.a.PatchRel32(over, g.a.Len())
	return nil
}

// fusedSelect chooses a when cond is true and b otherwise, reading EFLAGS
// directly. Mirrors selectOp without popping/testing a condition operand.
func (g *cg) fusedSelect(cond Cond, typed, isFloat bool) {
	b := g.pop()
	a := g.pop()
	if !typed {
		isFloat = g.isFloatOperand(a) || g.isFloatOperand(b)
	}
	if isFloat {
		dst := g.materializeF(a)
		keep := g.a.JccPlaceholder(cond) // cond true -> keep a
		src := g.materializeF(b)
		g.a.FMov(dst, src, true)
		g.freeFReg(src)
		g.a.PatchRel32(keep, g.a.Len())
		g.pushFReg(dst)
		return
	}
	dst := g.materialize(a)
	src := g.materialize(b)
	g.a.Cmovcc(invertCond(cond), dst, src, true) // cond false -> dst = b
	g.freeReg(src)
	g.pushReg(dst)
}
