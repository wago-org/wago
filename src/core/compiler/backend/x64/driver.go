package x64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// body walks the function's expression bytecode once, driving the operand stack:
// leaves (const, local.get) push lazily, binary ops push deferred nodes, and
// sinks (local.set, drop, return) condense. This is the port of WARP's
// Frontend.cpp parseCodeSection opcode switch. Phase 0 covers the integer const/
// local/ALU subset needed to prove the pipeline end-to-end.
func (f *fn) body(code []byte) error {
	r := wasm.NewReader(code)
	for len(f.ctrl) > 0 {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x00: // unreachable
			if !f.unreachable {
				f.emitTrap(trapUnreachable)
				f.unreachable = true
			}
		case 0x01: // nop
		case 0x02, 0x03, 0x04: // block / loop / if
			err = f.opBlock(r, op)
		case 0x05: // else
			err = f.opElse()
		case 0x0b: // end
			err = f.opEnd()
		case 0x0c, 0x0d: // br / br_if
			err = f.opBr(r, op == 0x0d)
		case 0x0e: // br_table
			err = f.opBrTable(r)
		case 0x0f: // return
			err = f.opReturn()
		default:
			if f.unreachable {
				err = skipImmediates(r, op)
			} else {
				err = f.emitPlain(r, op)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// emitPlain lowers a single non-control opcode (leaves, arithmetic, memory,
// conversions). Called only when reachable; dead code is skipped by the body loop.
func (f *fn) emitPlain(r *wasm.Reader, op byte) error {
	switch op {
	case 0x1a: // drop
		e := f.popValue()
		if e.st.kind == stReg {
			f.release(e.st.reg)
		}
	case 0x1b: // select
		f.emitSelect()
	case 0x1c: // select t (typed) — consume the declared result types
		n, err := r.U32()
		if err != nil {
			return err
		}
		for k := uint32(0); k < n; k++ {
			if _, err := r.Byte(); err != nil {
				return err
			}
		}
		f.emitSelect()

	case 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return err
		}
		f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(v)})
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return err
		}
		f.s.pushValue(storage{kind: stConst, typ: mtI64, cval: v})

	case 0x20: // local.get
		x, err := r.U32()
		if err != nil {
			return err
		}
		f.s.pushValue(storage{kind: stLocalRef, typ: f.localType[x], idx: int(x)})
	case 0x21, 0x22: // local.set / local.tee
		x, err := r.U32()
		if err != nil {
			return err
		}
		f.setLocal(int(x), op == 0x22)

	// i32 comparisons / eqz
	case 0x45:
		f.pushUnOp(opEqz, mtI32)
	case 0x46:
		f.pushBinOp(opEq, mtI32)
	case 0x47:
		f.pushBinOp(opNe, mtI32)
	case 0x48:
		f.pushBinOp(opLtS, mtI32)
	case 0x49:
		f.pushBinOp(opLtU, mtI32)
	case 0x4a:
		f.pushBinOp(opGtS, mtI32)
	case 0x4b:
		f.pushBinOp(opGtU, mtI32)
	case 0x4c:
		f.pushBinOp(opLeS, mtI32)
	case 0x4d:
		f.pushBinOp(opLeU, mtI32)
	case 0x4e:
		f.pushBinOp(opGeS, mtI32)
	case 0x4f:
		f.pushBinOp(opGeU, mtI32)

	// i64 comparisons / eqz
	case 0x50:
		f.pushUnOp(opEqz, mtI64)
	case 0x51:
		f.pushBinOp(opEq, mtI64)
	case 0x52:
		f.pushBinOp(opNe, mtI64)
	case 0x53:
		f.pushBinOp(opLtS, mtI64)
	case 0x54:
		f.pushBinOp(opLtU, mtI64)
	case 0x55:
		f.pushBinOp(opGtS, mtI64)
	case 0x56:
		f.pushBinOp(opGtU, mtI64)
	case 0x57:
		f.pushBinOp(opLeS, mtI64)
	case 0x58:
		f.pushBinOp(opLeU, mtI64)
	case 0x59:
		f.pushBinOp(opGeS, mtI64)
	case 0x5a:
		f.pushBinOp(opGeU, mtI64)

	// i32 unary
	case 0x67:
		f.pushUnOp(opClz, mtI32)
	case 0x68:
		f.pushUnOp(opCtz, mtI32)
	case 0x69:
		f.pushUnOp(opPopcnt, mtI32)

	// i32 arithmetic / logic / shift
	case 0x6a:
		f.pushBinOp(opAdd, mtI32)
	case 0x6b:
		f.pushBinOp(opSub, mtI32)
	case 0x6c:
		f.pushBinOp(opMul, mtI32)
	case 0x6d:
		f.pushBinOp(opDivS, mtI32)
	case 0x6e:
		f.pushBinOp(opDivU, mtI32)
	case 0x6f:
		f.pushBinOp(opRemS, mtI32)
	case 0x70:
		f.pushBinOp(opRemU, mtI32)
	case 0x71:
		f.pushBinOp(opAnd, mtI32)
	case 0x72:
		f.pushBinOp(opOr, mtI32)
	case 0x73:
		f.pushBinOp(opXor, mtI32)
	case 0x74:
		f.pushBinOp(opShl, mtI32)
	case 0x75:
		f.pushBinOp(opShrS, mtI32)
	case 0x76:
		f.pushBinOp(opShrU, mtI32)
	case 0x77:
		f.pushBinOp(opRotl, mtI32)
	case 0x78:
		f.pushBinOp(opRotr, mtI32)

	// i64 unary
	case 0x79:
		f.pushUnOp(opClz, mtI64)
	case 0x7a:
		f.pushUnOp(opCtz, mtI64)
	case 0x7b:
		f.pushUnOp(opPopcnt, mtI64)

	// i64 arithmetic / logic / shift
	case 0x7c:
		f.pushBinOp(opAdd, mtI64)
	case 0x7d:
		f.pushBinOp(opSub, mtI64)
	case 0x7e:
		f.pushBinOp(opMul, mtI64)
	case 0x7f:
		f.pushBinOp(opDivS, mtI64)
	case 0x80:
		f.pushBinOp(opDivU, mtI64)
	case 0x81:
		f.pushBinOp(opRemS, mtI64)
	case 0x82:
		f.pushBinOp(opRemU, mtI64)
	case 0x83:
		f.pushBinOp(opAnd, mtI64)
	case 0x84:
		f.pushBinOp(opOr, mtI64)
	case 0x85:
		f.pushBinOp(opXor, mtI64)
	case 0x86:
		f.pushBinOp(opShl, mtI64)
	case 0x87:
		f.pushBinOp(opShrS, mtI64)
	case 0x88:
		f.pushBinOp(opShrU, mtI64)
	case 0x89:
		f.pushBinOp(opRotl, mtI64)
	case 0x8a:
		f.pushBinOp(opRotr, mtI64)

	// linear memory: loads
	case 0x28: // i32.load
		if err := f.memLoad(r, 4, false, false); err != nil {
			return err
		}
	case 0x29: // i64.load
		if err := f.memLoad(r, 8, false, true); err != nil {
			return err
		}
	case 0x2c: // i32.load8_s
		if err := f.memLoad(r, 1, true, false); err != nil {
			return err
		}
	case 0x2d: // i32.load8_u
		if err := f.memLoad(r, 1, false, false); err != nil {
			return err
		}
	case 0x2e: // i32.load16_s
		if err := f.memLoad(r, 2, true, false); err != nil {
			return err
		}
	case 0x2f: // i32.load16_u
		if err := f.memLoad(r, 2, false, false); err != nil {
			return err
		}
	case 0x30: // i64.load8_s
		if err := f.memLoad(r, 1, true, true); err != nil {
			return err
		}
	case 0x31: // i64.load8_u
		if err := f.memLoad(r, 1, false, true); err != nil {
			return err
		}
	case 0x32: // i64.load16_s
		if err := f.memLoad(r, 2, true, true); err != nil {
			return err
		}
	case 0x33: // i64.load16_u
		if err := f.memLoad(r, 2, false, true); err != nil {
			return err
		}
	case 0x34: // i64.load32_s
		if err := f.memLoad(r, 4, true, true); err != nil {
			return err
		}
	case 0x35: // i64.load32_u
		if err := f.memLoad(r, 4, false, true); err != nil {
			return err
		}

	// linear memory: stores
	case 0x36: // i32.store
		if err := f.memStore(r, 4); err != nil {
			return err
		}
	case 0x37: // i64.store
		if err := f.memStore(r, 8); err != nil {
			return err
		}
	case 0x3a: // i32.store8
		if err := f.memStore(r, 1); err != nil {
			return err
		}
	case 0x3b: // i32.store16
		if err := f.memStore(r, 2); err != nil {
			return err
		}
	case 0x3c: // i64.store8
		if err := f.memStore(r, 1); err != nil {
			return err
		}
	case 0x3d: // i64.store16
		if err := f.memStore(r, 2); err != nil {
			return err
		}
	case 0x3e: // i64.store32
		if err := f.memStore(r, 4); err != nil {
			return err
		}

	// linear memory: size / grow
	case 0x3f:
		if err := f.memorySize(r); err != nil {
			return err
		}
	case 0x40:
		if err := f.memoryGrow(r); err != nil {
			return err
		}

	// width conversions / sign extensions
	case 0xa7: // i32.wrap_i64
		f.pushUnOp(opWrap, mtI32)
	case 0xac: // i64.extend_i32_s
		f.pushUnOp(opSExt32, mtI64)
	case 0xad: // i64.extend_i32_u
		f.pushUnOp(opZExt32, mtI64)
	case 0xc0: // i32.extend8_s
		f.pushUnOp(opSExt8, mtI32)
	case 0xc1: // i32.extend16_s
		f.pushUnOp(opSExt16, mtI32)
	case 0xc2: // i64.extend8_s
		f.pushUnOp(opSExt8, mtI64)
	case 0xc3: // i64.extend16_s
		f.pushUnOp(opSExt16, mtI64)
	case 0xc4: // i64.extend32_s
		f.pushUnOp(opSExt32, mtI64)

	default:
		return fmt.Errorf("x64: unsupported opcode 0x%02x", op)
	}
	return nil
}

// popValue removes the top valent block from the stack as a concrete value,
// condensing a deferred node first. The returned elem's storage is live.
func (f *fn) popValue() *elem {
	e := f.s.back()
	if e.isDeferred() {
		f.condense(e, regNone)
	}
	f.s.erase(e)
	return e
}

// emitSelect lowers `select`: result = cond != 0 ? a : b, where the operand
// stack holds a, then b, then cond on top. Lowered to test + cmove (if cond == 0,
// move b into a). Materialized eagerly (select is a sink for its operands).
func (f *fn) emitSelect() {
	cond := f.popValue()
	condReg := f.materialize(cond)
	f.pinned = f.pinned.add(condReg)
	b := f.popValue()
	bReg := f.materialize(b)
	f.pinned = f.pinned.add(bReg)
	a := f.popValue()
	aReg := f.materialize(a)

	w := a.st.typ.is64()
	f.a.TestSelf(condReg, false) // condition is i32
	f.a.Cmovcc(condE, aReg, bReg, w)

	f.pinned = f.pinned.remove(condReg)
	f.pinned = f.pinned.remove(bReg)
	f.release(condReg)
	f.release(bReg)

	e := f.s.pushValue(storage{kind: stReg, typ: a.st.typ, reg: aReg})
	f.regUser[aReg] = e
}

// setLocal stores the top-of-stack value into local x. For local.tee the value
// stays on the stack. Phase 0 keeps locals frame-resident (no register hint yet);
// register-resident locals (WARP's recoverLocalToReg) come with the fuller
// allocator.
func (f *fn) setLocal(x int, tee bool) {
	e := f.s.back()
	if e.isDeferred() {
		f.condense(e, regNone)
	}
	r := f.materialize(e)
	f.a.Store64(RBP, f.localOff(x), r)
	if !tee {
		f.s.erase(e)
		f.release(r)
	}
}
