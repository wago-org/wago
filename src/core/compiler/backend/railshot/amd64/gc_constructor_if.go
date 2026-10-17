//go:build amd64 && (!tinygo || !wago_lean)

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize only a terminal local.set/if/field-get pair. The object cannot
// escape, and neither arm changes locals or performs work other than its get.
// The allocation still runs through the existing dead-object charge/fallback.
// Reject larger field sets before walking constructor operands; the reader and
// both operand walks have fixed bounds and allocate no compiler metadata.
func (f *fn) tryForwardGCConstructorIf(reader *wasm.Reader, sourceType uint32, source wasm.SubType) ([3]storage, wasm.Reader, bool) {
	var values [3]storage
	fail := func() ([3]storage, wasm.Reader, bool) { return values, wasm.Reader{}, false }
	fieldN := len(source.Comp.Fields)
	if f.unreachable || f.inlineDepth != 0 || len(f.ctrl) != 1 || f.moduleEH ||
		len(f.customInstructions) != 0 || fieldN == 0 || fieldN > 16 ||
		fieldN > f.depth() || reader.BytesLeft() > 64 {
		return fail()
	}
	if !gcStructHasOnlyDirectScalars(source) {
		return fail()
	}
	r := *reader
	readByte := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	if !readByte(0x21) { // local.set object
		return fail()
	}
	objectLocal, err := r.U32()
	if err != nil {
		return fail()
	}
	op, err := r.Byte()
	if err != nil {
		return fail()
	}
	switch op {
	case 0x20: // unchanged local condition
		local, err := r.U32()
		if err != nil || local == objectLocal {
			return fail()
		}
		values[2] = storage{kind: stLocalRef, typ: mtI32, idx: local}
	case 0x41:
		v, err := r.I32()
		if err != nil {
			return fail()
		}
		values[2] = storage{kind: stConst, typ: mtI32, cval: int64(v)}
	default:
		return fail()
	}
	if !readByte(0x04) { // if with exactly one scalar result
		return fail()
	}
	result, err := r.Byte()
	if err != nil || (result != 0x7f && result != 0x7e && result != 0x7d && result != 0x7c) {
		return fail()
	}
	for arm := range 2 {
		if !readByte(0x20) {
			return fail()
		}
		local, err := r.U32()
		if err != nil || local != objectLocal || !readByte(0xfb) || !readByte(2) {
			return fail()
		}
		typ, err := r.U32()
		if err != nil || typ != sourceType {
			return fail()
		}
		field, err := r.U32()
		if err != nil || field >= uint32(fieldN) {
			return fail()
		}
		value := f.s.back()
		for i := fieldN - 1; i > int(field); i-- {
			if !value.isValue() {
				return fail()
			}
			value = value.prev
		}
		if !value.isValue() {
			return fail()
		}
		v := value.st
		switch v.kind {
		case stConst:
		case stLocalRef, stLocalReg:
			if v.idx == objectLocal {
				return fail()
			}
			v.kind, v.reg = stLocalRef, regNone
		default:
			return fail()
		}
		values[arm] = v
		end := byte(0x05)
		if arm == 1 {
			end = 0x0b
		}
		if !readByte(end) {
			return fail()
		}
	}
	// There is no later read of the skipped object local, even on a backedge.
	if r.BytesLeft() != 1 {
		return fail()
	}
	if end, ok := r.Peek(); !ok || end != 0x0b {
		return fail()
	}
	return values, r, true
}
