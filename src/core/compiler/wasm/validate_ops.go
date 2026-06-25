package wasm

func sameTypes(a, b []ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (v *validator) branch(depth uint32) ([]ValType, error) {
	if int(depth) >= len(v.ctrls) {
		return nil, v.verr(ErrUnknownLabel)
	}
	f := &v.ctrls[len(v.ctrls)-1-int(depth)]
	return labelTypes(f), nil
}

func (v *validator) checkLabel(lt []ValType) error {
	if err := v.popVals(lt); err != nil {
		return err
	}
	v.pushVals(lt)
	return nil
}

func (v *validator) readBlockType() (in, out []ValType, err error) {
	if !v.r.HasNext() {
		return nil, nil, v.verr(ErrInvalidBlockType)
	}
	first := v.r.data[v.r.pos]
	if first == 0x40 { // empty
		v.r.pos++
		return nil, nil, nil
	}
	if isValType(ValType(first)) { // single result
		v.r.pos++
		return nil, []ValType{ValType(first)}, nil
	}
	x, e := v.r.I64() // type index (s33)
	if e != nil {
		return nil, nil, e
	}
	if x < 0 || int(x) >= len(v.m.Types) {
		return nil, nil, v.verr(ErrInvalidBlockType)
	}
	ft := &v.m.Types[x]
	return ft.Params, ft.Results, nil
}

func (v *validator) readMemarg(naturalLog2 uint32) error {
	if v.memCount == 0 {
		return v.verr(ErrUnknownMemory)
	}
	align, err := v.r.U32()
	if err != nil {
		return err
	}
	if _, err := v.r.U32(); err != nil { // offset
		return err
	}
	if align > naturalLog2 {
		return v.verr(ErrInvalidAlignment)
	}
	return nil
}

var loadInfo = map[byte]struct {
	t    ValType
	algn uint32
}{
	0x28: {I32, 2}, 0x29: {I64, 3}, 0x2A: {F32, 2}, 0x2B: {F64, 3},
	0x2C: {I32, 0}, 0x2D: {I32, 0}, 0x2E: {I32, 1}, 0x2F: {I32, 1},
	0x30: {I64, 0}, 0x31: {I64, 0}, 0x32: {I64, 1}, 0x33: {I64, 1},
	0x34: {I64, 2}, 0x35: {I64, 2},
}

var storeInfo = map[byte]struct {
	t    ValType
	algn uint32
}{
	0x36: {I32, 2}, 0x37: {I64, 3}, 0x38: {F32, 2}, 0x39: {F64, 3},
	0x3A: {I32, 0}, 0x3B: {I32, 1},
	0x3C: {I64, 0}, 0x3D: {I64, 1}, 0x3E: {I64, 2},
}

// step consumes one instruction and updates the abstract operand/control stacks.
func (v *validator) step(op byte) error {
	switch {
	case op == 0x00: // unreachable
		v.setUnreachable()
	case op == 0x01: // nop
	case op == 0x02, op == 0x03, op == 0x04: // block / loop / if
		if op == 0x04 {
			if err := v.popT(I32); err != nil {
				return err
			}
		}
		in, out, err := v.readBlockType()
		if err != nil {
			return err
		}
		kind := ckBlock
		if op == 0x03 {
			kind = ckLoop
		} else if op == 0x04 {
			kind = ckIf
		}
		return v.pushCtrl(kind, in, out)
	case op == 0x05: // else
		f, err := v.popCtrl()
		if err != nil {
			return err
		}
		if f.kind != ckIf {
			return v.verr(ErrTypeMismatch)
		}
		return v.pushCtrl(ckElse, f.in, f.out)
	case op == 0x0B: // end
		f, err := v.popCtrl()
		if err != nil {
			return err
		}
		if f.kind == ckIf && !sameTypes(f.in, f.out) {
			return v.verr(ErrTypeMismatch) // if without else must be [t]->[t]
		}
		v.pushVals(f.out)
	case op == 0x0C: // br
		l, err := v.r.U32()
		if err != nil {
			return err
		}
		lt, err := v.branch(l)
		if err != nil {
			return err
		}
		if err := v.popVals(lt); err != nil {
			return err
		}
		v.setUnreachable()
	case op == 0x0D: // br_if
		if err := v.popT(I32); err != nil {
			return err
		}
		l, err := v.r.U32()
		if err != nil {
			return err
		}
		lt, err := v.branch(l)
		if err != nil {
			return err
		}
		if err := v.popVals(lt); err != nil {
			return err
		}
		v.pushVals(lt)
	case op == 0x0E: // br_table
		if err := v.popT(I32); err != nil {
			return err
		}
		n, err := v.r.U32()
		if err != nil {
			return err
		}
		labels := make([]uint32, n)
		for i := range labels {
			if labels[i], err = v.r.U32(); err != nil {
				return err
			}
		}
		def, err := v.r.U32()
		if err != nil {
			return err
		}
		dt, err := v.branch(def)
		if err != nil {
			return err
		}
		for _, l := range labels {
			lt, err := v.branch(l)
			if err != nil {
				return err
			}
			if len(lt) != len(dt) {
				return v.verr(ErrTypeMismatch)
			}
			if err := v.checkLabel(lt); err != nil {
				return err
			}
		}
		if err := v.popVals(dt); err != nil {
			return err
		}
		v.setUnreachable()
	case op == 0x0F: // return
		if err := v.popVals(v.ctrls[0].out); err != nil {
			return err
		}
		v.setUnreachable()
	case op == 0x10: // call
		fi, err := v.r.U32()
		if err != nil {
			return err
		}
		ft, ok := v.m.funcType(fi)
		if !ok {
			return v.verr(ErrUnknownFunc)
		}
		if err := v.popVals(ft.Params); err != nil {
			return err
		}
		v.pushVals(ft.Results)
	case op == 0x11: // call_indirect
		ti, err := v.r.U32()
		if err != nil {
			return err
		}
		tbl, err := v.r.U32()
		if err != nil {
			return err
		}
		tt, ok := v.m.tableType(tbl)
		if !ok {
			return v.verr(ErrUnknownTable)
		}
		if tt.Elem != FuncRef {
			return v.verr(ErrTypeMismatch) // call_indirect requires a funcref table
		}
		if int(ti) >= len(v.m.Types) {
			return v.verr(ErrUnknownType)
		}
		if err := v.popT(I32); err != nil {
			return err
		}
		ft := &v.m.Types[ti]
		if err := v.popVals(ft.Params); err != nil {
			return err
		}
		v.pushVals(ft.Results)
	case op == 0x1A: // drop
		if _, err := v.popVal(); err != nil {
			return err
		}
	case op == 0x1B: // select (untyped)
		if err := v.popT(I32); err != nil {
			return err
		}
		t1, err := v.popVal()
		if err != nil {
			return err
		}
		t2, err := v.popVal()
		if err != nil {
			return err
		}
		res := t1
		if t1 == vtUnknown {
			res = t2
		} else if t2 != vtUnknown && t1 != t2 {
			return v.verr(ErrTypeMismatch)
		}
		if res != vtUnknown && isRefType(ValType(res)) {
			return v.verr(ErrTypeMismatch) // untyped select forbids reference types
		}
		v.pushVal(res)
	case op == 0x1C: // select t
		n, err := v.r.U32()
		if err != nil {
			return err
		}
		if n != 1 {
			return v.verr(ErrInvalidResultArity)
		}
		t, err := readValType(v.r)
		if err != nil {
			return err
		}
		if err := v.popT(I32); err != nil {
			return err
		}
		if err := v.popT(t); err != nil {
			return err
		}
		if err := v.popT(t); err != nil {
			return err
		}
		v.pushT(t)
	case op == 0x20, op == 0x21, op == 0x22: // local.get/set/tee
		x, err := v.r.U32()
		if err != nil {
			return err
		}
		if int(x) >= len(v.locals) {
			return v.verr(ErrUnknownLocal)
		}
		lt := v.locals[x]
		switch op {
		case 0x20:
			v.pushT(lt)
		case 0x21:
			if err := v.popT(lt); err != nil {
				return err
			}
		case 0x22:
			if err := v.popT(lt); err != nil {
				return err
			}
			v.pushT(lt)
		}
	case op == 0x23: // global.get
		x, err := v.r.U32()
		if err != nil {
			return err
		}
		gt, ok := v.m.globalType(x)
		if !ok {
			return v.verr(ErrUnknownGlobal)
		}
		v.pushT(gt.Val)
	case op == 0x24: // global.set
		x, err := v.r.U32()
		if err != nil {
			return err
		}
		gt, ok := v.m.globalType(x)
		if !ok {
			return v.verr(ErrUnknownGlobal)
		}
		if !gt.Mutable {
			return v.verr(ErrImmutableGlobal)
		}
		return v.popT(gt.Val)
	case op >= 0x28 && op <= 0x35: // loads
		li := loadInfo[op]
		if err := v.readMemarg(li.algn); err != nil {
			return err
		}
		if err := v.popT(I32); err != nil {
			return err
		}
		v.pushT(li.t)
	case op >= 0x36 && op <= 0x3E: // stores
		si := storeInfo[op]
		if err := v.readMemarg(si.algn); err != nil {
			return err
		}
		if err := v.popT(si.t); err != nil {
			return err
		}
		if err := v.popT(I32); err != nil {
			return err
		}
	case op == 0x3F: // memory.size
		if _, err := v.r.Byte(); err != nil { // memidx
			return err
		}
		if v.memCount == 0 {
			return v.verr(ErrUnknownMemory)
		}
		v.pushT(I32)
	case op == 0x40: // memory.grow
		if _, err := v.r.Byte(); err != nil {
			return err
		}
		if v.memCount == 0 {
			return v.verr(ErrUnknownMemory)
		}
		if err := v.popT(I32); err != nil {
			return err
		}
		v.pushT(I32)
	case op == 0x41: // i32.const
		if _, err := v.r.I32(); err != nil {
			return err
		}
		v.pushT(I32)
	case op == 0x42: // i64.const
		if _, err := v.r.I64(); err != nil {
			return err
		}
		v.pushT(I64)
	case op == 0x43: // f32.const
		if err := v.r.Step(4); err != nil {
			return err
		}
		v.pushT(F32)
	case op == 0x44: // f64.const
		if err := v.r.Step(8); err != nil {
			return err
		}
		v.pushT(F64)
	case op == 0x45:
		return v.testop(I32)
	case op >= 0x46 && op <= 0x4F:
		return v.cmp(I32)
	case op == 0x50:
		return v.testop(I64)
	case op >= 0x51 && op <= 0x5A:
		return v.cmp(I64)
	case op >= 0x5B && op <= 0x60:
		return v.cmp(F32)
	case op >= 0x61 && op <= 0x66:
		return v.cmp(F64)
	case op >= 0x67 && op <= 0x69:
		return v.unop(I32)
	case op >= 0x6A && op <= 0x78:
		return v.binop(I32)
	case op >= 0x79 && op <= 0x7B:
		return v.unop(I64)
	case op >= 0x7C && op <= 0x8A:
		return v.binop(I64)
	case op >= 0x8B && op <= 0x91:
		return v.unop(F32)
	case op >= 0x92 && op <= 0x98:
		return v.binop(F32)
	case op >= 0x99 && op <= 0x9F:
		return v.unop(F64)
	case op >= 0xA0 && op <= 0xA6:
		return v.binop(F64)
	case op == 0xA7:
		return v.cvt(I64, I32)
	case op >= 0xA8 && op <= 0xAB:
		return v.cvt(typeForFloat(op), I32)
	case op == 0xAC, op == 0xAD:
		return v.cvt(I32, I64)
	case op >= 0xAE && op <= 0xB1:
		return v.cvt(typeForFloat(op), I64)
	case op == 0xB2, op == 0xB3:
		return v.cvt(I32, F32)
	case op == 0xB4, op == 0xB5:
		return v.cvt(I64, F32)
	case op == 0xB6:
		return v.cvt(F64, F32)
	case op == 0xB7, op == 0xB8:
		return v.cvt(I32, F64)
	case op == 0xB9, op == 0xBA:
		return v.cvt(I64, F64)
	case op == 0xBB:
		return v.cvt(F32, F64)
	case op == 0xBC:
		return v.cvt(F32, I32) // i32.reinterpret_f32
	case op == 0xBD:
		return v.cvt(F64, I64)
	case op == 0xBE:
		return v.cvt(I32, F32)
	case op == 0xBF:
		return v.cvt(I64, F64)
	case op == 0xC0, op == 0xC1:
		return v.unop(I32) // i32.extend8_s / extend16_s
	case op >= 0xC2 && op <= 0xC4:
		return v.unop(I64) // i64.extend{8,16,32}_s
	case op == 0xD0: // ref.null t
		t, err := readValType(v.r)
		if err != nil {
			return err
		}
		if !isRefType(t) {
			return v.verr(ErrInvalidValType)
		}
		v.pushT(t)
	case op == 0xD1: // ref.is_null
		t, err := v.popVal()
		if err != nil {
			return err
		}
		if t != vtUnknown && !isRefType(ValType(t)) {
			return v.verr(ErrTypeMismatch)
		}
		v.pushT(I32)
	case op == 0xD2: // ref.func x
		if _, err := v.r.U32(); err != nil {
			return err
		}
		v.pushT(FuncRef)
	case op == 0xFC:
		return v.stepFC()
	default:
		return v.verr(ErrUnsupportedOpcode)
	}
	return nil
}

// typeForFloat maps an f32-vs-f64 trunc opcode to its source float type.
// Used for the trunc families where _s/_u pairs alternate f32 then f64.
func typeForFloat(op byte) ValType {
	switch op {
	case 0xA8, 0xA9, 0xAE, 0xAF: // *_f32_*
		return F32
	default: // 0xAA,0xAB,0xB0,0xB1: *_f64_*
		return F64
	}
}

// stepFC handles 0xFC-prefixed saturating-truncation and bulk-memory ops.
func (v *validator) stepFC() error {
	sub, err := v.r.U32()
	if err != nil {
		return err
	}
	switch sub {
	case 0, 1: // i32.trunc_sat_f32_s/u
		return v.cvt(F32, I32)
	case 2, 3: // i32.trunc_sat_f64_s/u
		return v.cvt(F64, I32)
	case 4, 5: // i64.trunc_sat_f32_s/u
		return v.cvt(F32, I64)
	case 6, 7: // i64.trunc_sat_f64_s/u
		return v.cvt(F64, I64)
	case 8: // memory.init dataidx memidx
		if _, err := v.r.U32(); err != nil {
			return err
		}
		if _, err := v.r.Byte(); err != nil {
			return err
		}
		return v.popThreeI32(true)
	case 9: // data.drop dataidx
		_, err := v.r.U32()
		return err
	case 10: // memory.copy dst src
		if _, err := v.r.Byte(); err != nil {
			return err
		}
		if _, err := v.r.Byte(); err != nil {
			return err
		}
		return v.popThreeI32(true)
	case 11: // memory.fill mem
		if _, err := v.r.Byte(); err != nil {
			return err
		}
		return v.popThreeI32(true)
	default:
		return v.verr(ErrUnsupportedOpcode)
	}
}

func (v *validator) popThreeI32(needMem bool) error {
	if needMem && v.memCount == 0 {
		return v.verr(ErrUnknownMemory)
	}
	for i := 0; i < 3; i++ {
		if err := v.popT(I32); err != nil {
			return err
		}
	}
	return nil
}
