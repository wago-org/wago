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
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x0b: // end (function-level in Phase 0)
			return nil
		case 0x1a: // drop
			e := f.popValue()
			if e.st.kind == stReg {
				f.release(e.st.reg)
			}

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

		default:
			return fmt.Errorf("x64: unsupported opcode 0x%02x (Phase 1)", op)
		}
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
