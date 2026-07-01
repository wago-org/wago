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

		// integer ALU (i32 then i64) — the deferred set for Phase 0.
		case 0x6a:
			f.pushBinOp(opAdd, mtI32)
		case 0x6b:
			f.pushBinOp(opSub, mtI32)
		case 0x71:
			f.pushBinOp(opAnd, mtI32)
		case 0x72:
			f.pushBinOp(opOr, mtI32)
		case 0x73:
			f.pushBinOp(opXor, mtI32)
		case 0x7c:
			f.pushBinOp(opAdd, mtI64)
		case 0x7d:
			f.pushBinOp(opSub, mtI64)
		case 0x83:
			f.pushBinOp(opAnd, mtI64)
		case 0x84:
			f.pushBinOp(opOr, mtI64)
		case 0x85:
			f.pushBinOp(opXor, mtI64)

		default:
			return fmt.Errorf("x64: unsupported opcode 0x%02x (Phase 0)", op)
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
