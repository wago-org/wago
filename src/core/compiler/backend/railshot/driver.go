package amd64

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
	return f.bodyLoop(wasm.NewReader(code), 0)
}

// bodyLoop drives the opcode switch until the control stack shrinks to minCtrl.
// The function body runs with minCtrl=0 (until the function frame's end). An
// inlined callee with control flow runs with minCtrl = the depth just below its
// synthetic frame, so its terminating `end` (which pops that frame) ends the loop
// and returns control to the caller's body.
func (f *fn) bodyLoop(r *wasm.Reader, minCtrl int) error {
	for len(f.ctrl) > minCtrl {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x00: // unreachable
			if !f.unreachable {
				f.trapAlways(trapUnreachable)
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
	case 0x10: // call
		return f.callOp(r)
	case 0x11: // call_indirect
		return f.callIndirect(r)

	case 0x1a: // drop
		e := f.popValue()
		switch e.st.kind {
		case stReg:
			if e.st.typ.isXMM() {
				f.releaseF(e.st.reg)
			} else {
				f.release(e.st.reg)
			}
		case stMemRef:
			// In guard-page mode the load itself is the OOB trap, so a dropped load
			// must still be emitted; with explicit checks the bounds check already ran.
			if f.guardMode {
				if e.st.typ.isFloat() {
					x := f.allocFReg(0)
					f.loadFMemRef(x, e.st)
					f.releaseF(x)
				} else {
					r := f.memRefValue(e.st) // never write a borrowed address register
					f.release(r)
				}
			}
			f.releaseMemRef(e.st)
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
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(v)})
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return err
		}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: v})

	case 0x20: // local.get
		x32, err := r.U32()
		if err != nil {
			return err
		}
		x := uint32(int(x32) + f.localBase) // localBase remaps an inlined callee's locals; 0 otherwise
		if f.localConstZero(int(x)) {
			if pr, _, ok := f.pinReg(int(x)); ok {
				f.recoverLocal(int(x)) // materialize the lazy zero into the pinned register
				f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: int(x)})
			} else {
				f.pushValue(zeroStorage(f.localType[x]))
			}
		} else if pr, _, ok := f.pinReg(int(x)); ok {
			f.recoverLocal(int(x)) // reload lazily if it was spilled around a call
			f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: int(x)})
		} else {
			f.pushValue(storage{kind: stLocalRef, typ: f.localType[x], idx: int(x)})
		}
	case 0x21, 0x22: // local.set / local.tee
		x, err := r.U32()
		if err != nil {
			return err
		}
		f.setLocal(int(x)+f.localBase, op == 0x22) // localBase remaps an inlined callee's locals; 0 otherwise
	case 0x23: // global.get
		return f.globalGet(r)
	case 0x24: // global.set
		return f.globalSet(r)

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

	// --- floating point ---
	case 0x43: // f32.const
		bits, err := r.LEU32()
		if err != nil {
			return err
		}
		f.fconst(uint64(bits), mtF32)
	case 0x44: // f64.const
		bits, err := r.LEU64()
		if err != nil {
			return err
		}
		f.fconst(bits, mtF64)

	case 0x2a: // f32.load
		return f.fload(r, false)
	case 0x2b: // f64.load
		return f.fload(r, true)
	case 0x38: // f32.store
		return f.fstore(r, false)
	case 0x39: // f64.store
		return f.fstore(r, true)

	// f32 comparisons
	case 0x5b:
		f.fcmp(opEq, false)
	case 0x5c:
		f.fcmp(opNe, false)
	case 0x5d:
		f.fcmp(opLtS, false)
	case 0x5e:
		f.fcmp(opGtS, false)
	case 0x5f:
		f.fcmp(opLeS, false)
	case 0x60:
		f.fcmp(opGeS, false)
	// f64 comparisons
	case 0x61:
		f.fcmp(opEq, true)
	case 0x62:
		f.fcmp(opNe, true)
	case 0x63:
		f.fcmp(opLtS, true)
	case 0x64:
		f.fcmp(opGtS, true)
	case 0x65:
		f.fcmp(opLeS, true)
	case 0x66:
		f.fcmp(opGeS, true)

	// f32 unary/binary
	case 0x8b:
		f.fabs(false)
	case 0x8c:
		f.fneg(false)
	case 0x8d:
		f.fround(false, roundCeil)
	case 0x8e:
		f.fround(false, roundFloor)
	case 0x8f:
		f.fround(false, roundTrunc)
	case 0x90:
		f.fround(false, roundNearest)
	case 0x91:
		f.fsqrt(false)
	case 0x92:
		if done, err := f.tryFbinLocalSet(r, f.a.VFAdd, 0x58, false); done || err != nil {
			return err
		}
		f.fbin(f.a.VFAdd, 0x58, false)
	case 0x93:
		if done, err := f.tryFbinLocalSet(r, f.a.VFSub, 0x5C, false); done || err != nil {
			return err
		}
		f.fbin(f.a.VFSub, 0x5C, false)
	case 0x94:
		if done, err := f.tryFbinLocalSet(r, f.a.VFMul, 0x59, false); done || err != nil {
			return err
		}
		f.fbin(f.a.VFMul, 0x59, false)
	case 0x95:
		if done, err := f.tryFbinLocalSet(r, f.a.VFDiv, 0x5E, false); done || err != nil {
			return err
		}
		f.fbin(f.a.VFDiv, 0x5E, false)
	case 0x96:
		f.fminmax(false, false)
	case 0x97:
		f.fminmax(false, true)
	case 0x98:
		f.fcopysign(false)
	// f64 unary/binary
	case 0x99:
		f.fabs(true)
	case 0x9a:
		f.fneg(true)
	case 0x9b:
		f.fround(true, roundCeil)
	case 0x9c:
		f.fround(true, roundFloor)
	case 0x9d:
		f.fround(true, roundTrunc)
	case 0x9e:
		f.fround(true, roundNearest)
	case 0x9f:
		f.fsqrt(true)
	case 0xa0:
		if done, err := f.tryFbinLocalSet(r, f.a.VFAdd, 0x58, true); done || err != nil {
			return err
		}
		f.fbin(f.a.VFAdd, 0x58, true)
	case 0xa1:
		if done, err := f.tryFbinLocalSet(r, f.a.VFSub, 0x5C, true); done || err != nil {
			return err
		}
		f.fbin(f.a.VFSub, 0x5C, true)
	case 0xa2:
		if done, err := f.tryFbinLocalSet(r, f.a.VFMul, 0x59, true); done || err != nil {
			return err
		}
		f.fbin(f.a.VFMul, 0x59, true)
	case 0xa3:
		if done, err := f.tryFbinLocalSet(r, f.a.VFDiv, 0x5E, true); done || err != nil {
			return err
		}
		f.fbin(f.a.VFDiv, 0x5E, true)
	case 0xa4:
		f.fminmax(true, false)
	case 0xa5:
		f.fminmax(true, true)
	case 0xa6:
		f.fcopysign(true)

	// float→int truncation (trapping)
	case 0xa8:
		f.f2iTrunc(false, false, true) // i32.trunc_f32_s
	case 0xa9:
		f.f2iTrunc(false, false, false) // i32.trunc_f32_u
	case 0xaa:
		f.f2iTrunc(false, true, true) // i32.trunc_f64_s
	case 0xab:
		f.f2iTrunc(false, true, false) // i32.trunc_f64_u
	case 0xae:
		f.f2iTrunc(true, false, true) // i64.trunc_f32_s
	case 0xaf:
		f.f2iTrunc(true, false, false) // i64.trunc_f32_u
	case 0xb0:
		f.f2iTrunc(true, true, true) // i64.trunc_f64_s
	case 0xb1:
		f.f2iTrunc(true, true, false) // i64.trunc_f64_u

	// int→float conversion
	case 0xb2:
		f.i2f(false, false) // f32.convert_i32_s
	case 0xb3:
		f.i2fU(false, false) // f32.convert_i32_u
	case 0xb4:
		f.i2f(false, true) // f32.convert_i64_s
	case 0xb5:
		f.i2fU(false, true) // f32.convert_i64_u
	case 0xb6:
		f.fdemote() // f32.demote_f64
	case 0xb7:
		f.i2f(true, false) // f64.convert_i32_s
	case 0xb8:
		f.i2fU(true, false) // f64.convert_i32_u
	case 0xb9:
		f.i2f(true, true) // f64.convert_i64_s
	case 0xba:
		f.i2fU(true, true) // f64.convert_i64_u
	case 0xbb:
		f.fpromote() // f64.promote_f32

	// reinterpret
	case 0xbc:
		f.reinterpretFloatToInt(false) // i32.reinterpret_f32
	case 0xbd:
		f.reinterpretFloatToInt(true) // i64.reinterpret_f64
	case 0xbe:
		f.reinterpretIntToFloat(false) // f32.reinterpret_i32
	case 0xbf:
		f.reinterpretIntToFloat(true) // f64.reinterpret_i64

	case 0xfc: // misc (multi-byte) opcodes
		return f.emitFC(r)
	case 0xfd: // SIMD
		return f.emitFD(r)

	default:
		return fmt.Errorf("amd64: unsupported opcode 0x%02x", op)
	}
	return nil
}

// emitFC dispatches the 0xFC-prefixed opcodes: saturating truncation and bulk
// memory ops.
func (f *fn) emitFC(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 0:
		f.truncSat(false, false, true) // i32.trunc_sat_f32_s
	case 1:
		f.truncSat(false, false, false) // i32.trunc_sat_f32_u
	case 2:
		f.truncSat(true, false, true) // i32.trunc_sat_f64_s
	case 3:
		f.truncSat(true, false, false) // i32.trunc_sat_f64_u
	case 4:
		f.truncSat(false, true, true) // i64.trunc_sat_f32_s
	case 5:
		f.truncSat(false, true, false) // i64.trunc_sat_f32_u
	case 6:
		f.truncSat(true, true, true) // i64.trunc_sat_f64_s
	case 7:
		f.truncSat(true, true, false) // i64.trunc_sat_f64_u
	case 10: // memory.copy
		return f.memoryCopy(r)
	case 11: // memory.fill
		return f.memoryFill(r)
	default:
		return fmt.Errorf("amd64: unsupported 0xFC opcode %d", sub)
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
	f.erase(e)
	return e
}

func (f *fn) tryFbinLocalSet(r *wasm.Reader, vop func(dst, s1, s2 Reg, f64 bool), memOp byte, f64 bool) (bool, error) {
	save := r.Offset()
	op, ok := r.Peek()
	if !ok || (op != 0x21 && op != 0x22) {
		return false, nil
	}
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	x32, err := r.U32()
	if err != nil {
		return false, err
	}
	x := int(x32) + f.localBase
	pr, isFloat, pinned := f.pinReg(x)
	if !pinned || !isFloat || x < 0 || x >= len(f.localType) || f.localType[x] != mtOf2(f64) {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert()
	}
	right := f.s.back()
	if right == nil {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	left := baseOfValentBlock(right).prev
	f.realizeLocalRefs(x, left)
	f.fbinInto(pr, vop, memOp, f64)
	f.markLocalDirty(x)
	f.stats.peep("float-local-sink")
	if op == 0x22 {
		f.pushValue(storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x})
	}
	return true, nil
}

// emitSelect lowers `select`: result = cond != 0 ? a : b, where the operand
// stack holds a, then b, then cond on top. Lowered to test + cmove (if cond == 0,
// move b into a). Materialized eagerly (select is a sink for its operands).
func (f *fn) emitSelect() {
	// Flags-select: when the condition is a deferred relational/eqz compare and both
	// branches are integers, emit the compare's CMP and a CMOV on its flags directly
	// — skipping the SETcc + MOVZX + TEST that materializing the boolean costs. The
	// compare is condensed last (right before the CMOV), so its flags are live.
	if top := f.s.back(); isFusableCompare(top) && f.trySelectOnFlags(top) {
		return
	}
	cond := f.popValue()
	condReg := f.materialize(cond) // condition is i32
	f.pinned = f.pinned.add(condReg)
	b := f.popValue()
	a := f.popValue()

	// XMM operands have no cmov, so branch. Scalar floats use scalar moves;
	// v128 uses a full-vector copy. Integer operands use cmov.
	aV128 := a.kind == ekValue && a.st.typ.isV128()
	bV128 := b.kind == ekValue && b.st.typ.isV128()
	if aV128 || bV128 {
		aX := f.materializeV128(a)
		f.fpinned = f.fpinned.add(aX)
		bX := f.materializeV128(b)
		f.pinned = f.pinned.remove(condReg)
		f.a.TestSelf(condReg, false)
		skip := f.a.JccPlaceholder(condNE) // cond != 0 → keep a
		f.a.VMovdqu(aX, bX)                // cond == 0 → a = b (all 128 bits)
		f.a.PatchRel32(skip, f.a.Len())
		f.fpinned = f.fpinned.remove(aX)
		f.releaseF(bX)
		f.release(condReg)
		f.pushVReg(aX)
		return
	}

	aFloat := a.kind == ekValue && a.st.typ.isFloat()
	bFloat := b.kind == ekValue && b.st.typ.isFloat()
	if aFloat || bFloat {
		typ := a.st.typ
		if !typ.isFloat() {
			typ = b.st.typ
		}
		f64 := typ == mtF64
		aX := f.materializeF(a)
		f.fpinned = f.fpinned.add(aX)
		bX := f.materializeF(b)
		f.pinned = f.pinned.remove(condReg)
		f.a.TestSelf(condReg, false)
		skip := f.a.JccPlaceholder(condNE) // cond != 0 → keep a
		f.a.FMov(aX, bX, f64)              // cond == 0 → a = b
		f.a.PatchRel32(skip, f.a.Len())
		f.fpinned = f.fpinned.remove(aX)
		f.releaseF(bX)
		f.release(condReg)
		f.pushFReg(aX, typ)
		return
	}

	w := (a.kind == ekValue && a.st.typ.is64()) || (b.kind == ekValue && b.st.typ.is64())
	bReg := f.materialize(b)
	f.pinned = f.pinned.add(bReg)
	aReg := f.materialize(a)
	f.stats.peep("select-cmov")
	f.a.TestSelf(condReg, false)
	f.a.Cmovcc(condE, aReg, bReg, w) // cond == 0 → a = b
	f.pinned = f.pinned.remove(condReg)
	f.pinned = f.pinned.remove(bReg)
	f.release(condReg)
	f.release(bReg)
	f.pushReg(aReg, mtI32OrWide(w))
}

func mtI32OrWide(wide bool) machineType {
	if wide {
		return mtI64
	}
	return mtI32
}

// trySelectOnFlags lowers `select` on the flags of a fusable compare condition
// (cond, the top operand). It materializes the two integer branches into owned
// registers, emits the compare's CMP (which sets the flags last), and CMOVs —
// no SETcc/TEST. Returns false (leaving the operand stack untouched) when the
// branches are not both integer (floats/v128 have no CMOV) or the block shape is
// unexpected, so the caller falls back to the materialized-boolean path.
func (f *fn) trySelectOnFlags(cond *elem) bool {
	bRoot := baseOfValentBlock(cond).prev
	if bRoot == f.s.head {
		return false
	}
	aRoot := baseOfValentBlock(bRoot).prev
	if aRoot == f.s.head {
		return false
	}
	at, bt := rootMachineType(aRoot), rootMachineType(bRoot)
	if at.isFloat() || at.isV128() || bt.isFloat() || bt.isV128() {
		return false
	}
	w := at.is64() || bt.is64()
	// Materialize both branches into owned registers BEFORE the compare: their loads
	// clobber flags harmlessly (the CMP comes after and sets them cleanly), and they
	// are pinned so condensing the compare's operands cannot spill them.
	aReg := f.materialize(aRoot)
	f.pinned = f.pinned.add(aReg)
	bReg := f.materialize(bRoot)
	f.pinned = f.pinned.add(bReg)
	cc := f.condenseToFlags(cond) // emits the CMP (last flag-affecting insn), consumes cond
	f.stats.peep("select-flags")
	f.a.Cmovcc(invertCond(cc), aReg, bReg, w) // cond false → a = b
	f.pinned = f.pinned.remove(aReg)
	f.pinned = f.pinned.remove(bReg)
	f.release(bReg)
	f.erase(bRoot)
	f.erase(aRoot)
	f.pushReg(aReg, mtI32OrWide(w))
	return true
}

// setLocal stores the top-of-stack value into local x. For local.tee the value
// stays on the stack. Phase 0 keeps locals frame-resident (no register hint yet);
// register-resident locals (WARP's recoverLocalToReg) come with the fuller
// allocator.
// realizeLocalRefs forces any pending operand-stack references to local x into
// registers before x is overwritten, preserving wasm's semantics that a
// local.get reads the value at get-time (WARP recoverLocalToReg). A lazy
// stLocalRef is loaded; a deferred node whose subtree reads x is condensed.
func (f *fn) realizeLocalRefs(x int, skipFrom *elem) {
	// skipFrom (non-nil) marks the base of the value-being-set's valent block for
	// an in-place self-update (`local.set $x (binop (local.get $x) …)`): refs to x
	// inside that block are consumed directly into x's register by condenseInto, so
	// realizing them here would force the wasteful copy-out + copy-back. Refs BELOW
	// it still need x's pre-set value and are realized.
	for e := f.s.head.next; e != f.s.head; {
		if e == skipFrom {
			break
		}
		next := e.next
		switch {
		case e.kind == ekValue && (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.idx == x:
			f.materializeByType(e)
		case e.kind == ekValue && e.st.kind == stMemRef && e.st.memBorrow() == x:
			// A deferred load addressing through x's pinned register: emit it
			// before x is overwritten.
			f.materializeByType(e)
		case e.kind == ekDeferred && subtreeRefsLocal(e, x):
			f.condense(e, regNone)
		}
		e = next
	}
}

// subtreeRefsLocal reports whether the valent block rooted at e reads local x.
func subtreeRefsLocal(e *elem, x int) bool {
	if e == nil {
		return false
	}
	if e.kind == ekValue {
		return (e.st.kind == stLocalRef || e.st.kind == stLocalReg) && e.st.idx == x
	}
	if e.kind == ekDeferred {
		return subtreeRefsLocal(e.arg0, x) || subtreeRefsLocal(e.arg1, x)
	}
	return false
}

func (f *fn) setLocal(x int, tee bool) {
	if f.bcKind == 1 && f.bcIdx == uint32(x) {
		f.invalidateBoundsCert() // the certified base local changed value
	}
	e := f.s.back()
	// In-place self-update `local.set $x (binop (local.get $x) …)`: let condenseInto
	// consume the top expression straight into x's register instead of pre-copying
	// its (local.get $x) operand. condenseBinary handles an operand aliasing dest.
	var skipFrom *elem
	if !tee && e != nil && e.isDeferred() && isBinALU(e.op) {
		skipFrom = baseOfValentBlock(e)
	}
	f.realizeLocalRefs(x, skipFrom)
	if pr, isFloat, ok := f.pinReg(x); ok && !isFloat {
		// Register-pinned local: compute/load directly into the local's register.
		// condenseInto may temporarily mark pr as an owned result for deferred
		// expressions; clear that ownership because pinned-local registers are not
		// allocator scratch registers.
		f.condenseInto(e, pr)
		f.release(pr)
		f.markLocalDirty(x) // value now lives (only) in the register
		if tee {
			f.replaceStorage(e, storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x}) // borrowed ref stays
		} else {
			f.erase(e)
		}
		return
	}
	if pr, isFloat, ok := f.pinReg(x); ok && isFloat {
		// Register-pinned float local: move the value into its XMM register.
		f64 := f.localType[x] == mtF64
		if e.kind == ekValue && e.st.kind == stLocalReg {
			if e.st.reg != pr {
				f.a.FMov(pr, e.st.reg, f64) // borrowed float local → direct move
			}
		} else {
			xmm := f.materializeF(e)
			if xmm != pr {
				f.a.FMov(pr, xmm, f64)
			}
			f.releaseF(xmm)
		}
		f.markLocalDirty(x)
		if tee {
			f.replaceStorage(e, storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: x})
		} else {
			f.erase(e)
		}
		return
	}
	if f.localType[x] == mtV128 {
		xmm := f.materializeV128(e)
		f.a.VMovdquStoreDisp(RSP, f.localOff(x), xmm)
		f.locals[x].state = lsMem
		if !tee {
			f.erase(e)
			f.releaseF(xmm)
		}
		return
	}
	if f.localType[x].isFloat() {
		xmm := f.materializeF(e)
		f.a.FStoreDisp(RSP, f.localOff(x), xmm, f.localType[x] == mtF64)
		f.locals[x].state = lsMem
		if !tee {
			f.erase(e)
			f.releaseF(xmm)
		}
		return
	}
	if e.isDeferred() {
		f.condense(e, regNone)
	}
	r := f.materialize(e)
	f.a.Store64(RSP, f.localOff(x), r)
	f.locals[x].state = lsMem
	if !tee {
		f.erase(e)
		f.release(r)
	}
}
