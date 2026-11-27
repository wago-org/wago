// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"sync"

	"github.com/wago-org/wago/internal/buildfeatures"
)

// beginBranchTable advances the generation used to stamp distinct target
// frames. A decoded code section cannot contain enough instructions to exhaust
// uint32. The overflow branch defensively resets the active frame set.
func (v *funcValidator) beginBranchTable() {
	v.branchTableEpoch++
	if v.branchTableEpoch == 0 {
		for i := range v.ctrls {
			v.ctrls[i].branchTableEpoch = 0
		}
		v.branchTableEpoch = 1
	}
}

// markBranchTableLabel is called only after label has been bounds-checked.
func (v *funcValidator) markBranchTableLabel(label uint32) bool {
	f := &v.ctrls[len(v.ctrls)-1-int(label)]
	fresh := f.branchTableEpoch != v.branchTableEpoch
	f.branchTableEpoch = v.branchTableEpoch
	return fresh
}

// step validates one already-decoded instruction. in is taken by pointer: the
// Instruction struct is ~56 bytes and this is the validator's innermost hot path,
// so passing a value here shows up as runtime.duffcopy under profiling.
func (v *funcValidator) step(in *Instruction) error {
	if in.Kind >= numInstrKinds {
		return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
	}
	if buildfeatures.SignExtensionOps == 0 && in.Kind >= InstrI32Extend8S && in.Kind <= InstrI64Extend32S {
		return v.verr(ErrUnsupportedFeature, "sign-extension-ops is disabled in this build")
	}
	if buildfeatures.TailCall == 0 {
		switch in.Kind {
		case InstrReturnCall, InstrReturnCallIndirect, InstrReturnCallRef:
			return v.verr(ErrUnsupportedFeature, "tail-call is disabled in this build")
		}
	}
	if v.constOnly && !isConstInstruction(in.Kind) && !(v.features.GCConstExpr && (in.Kind == InstrStructNew || in.Kind == InstrArrayNew || in.Kind == InstrArrayNewDefault || in.Kind == InstrRefI31 || in.Kind == InstrAnyConvertExtern || in.Kind == InstrExternConvertAny)) {
		return v.verr(ErrConstExprRequired, in.Kind.String())
	}
	for _, t := range in.ValTypes() {
		if err := v.validateValType(t); err != nil {
			return err
		}
	}
	if e := scalarOpEffect(in.Kind); e.cat == effLoad {
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(e.a.valType())
		return nil
	} else if e.cat == effStore {
		addr, err := v.checkMemArg(in.MemArg(), uint32(e.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(e.a.valType()); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	switch in.Kind {
	case InstrUnreachable:
		v.unreachable()
	case InstrNop:
	case InstrBlock, InstrLoop:
		ins, outs, err := v.blockSig(in.BlockType())
		if err != nil {
			return err
		}
		kind := ctrlBlock
		if in.Kind == InstrLoop {
			kind = ctrlLoop
		}
		if err := v.pushCtrl(kind, ins, outs); err != nil {
			return err
		}
		for _, child := range in.Body().Instrs {
			if err := v.step(&child); err != nil {
				return err
			}
		}
		_, err = v.popCtrl()
		return err
	case InstrIf:
		// Resolve and validate the block type before inspecting operands. Invalid
		// type indexes are declaration errors even when the condition stack is also
		// malformed, matching the Core validation order.
		ins, outs, err := v.blockSig(in.BlockType())
		if err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		baseVals := append([]val(nil), v.vals...)
		baseCtrls := append([]ctrlFrame(nil), v.ctrls...)
		if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
			return err
		}
		for _, child := range in.Then() {
			if err := v.step(&child); err != nil {
				return err
			}
		}
		_, err = v.popCtrl()
		if err != nil {
			return err
		}
		thenVals := append([]val(nil), v.vals...)
		v.vals = baseVals
		v.ctrls = baseCtrls
		if len(in.Else()) > 0 {
			if err := v.pushCtrl(ctrlIf, ins, outs); err != nil {
				return err
			}
			for _, child := range in.Else() {
				if err := v.step(&child); err != nil {
					return err
				}
			}
			_, err = v.popCtrl()
			if err != nil {
				return err
			}
		} else if !v.matchValTypes(ins, outs) {
			// With no else arm, the false path preserves the block inputs as the
			// expression results, including reference type widening.
			return v.verr(ErrTypeMismatch, "if without else")
		}
		if len(in.Else()) > 0 && len(v.vals) != len(thenVals) {
			return v.verr(ErrTypeMismatch, "if branch heights")
		}
		v.vals = thenVals
	case InstrBr, InstrBrIf:
		return v.stepBranch(in.Kind, in.Index)
	case InstrBrTable:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		dt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		payloadHeight := len(v.vals)
		v.beginBranchTable()
		v.markBranchTableLabel(in.Index)
		for _, l := range in.Indices() {
			lt, err := v.label(l)
			if err != nil {
				return err
			}
			if !v.markBranchTableLabel(l) {
				continue
			}
			if len(lt) != len(dt) {
				return v.verr(ErrTypeMismatch, "br_table label arity")
			}
			// Every target consumes the same available branch payload. Restore the
			// values after each check so an unreachable-stack bottom can match
			// heterogeneous equal-arity labels without weakening reachable values.
			if err := v.popAll(lt); err != nil {
				return err
			}
			v.vals = v.vals[:payloadHeight]
		}
		if err := v.popAll(dt); err != nil {
			return err
		}
		v.unreachable()
	case InstrReturn:
		if err := v.popAll(v.ctrls[0].out); err != nil {
			return err
		}
		v.unreachable()
	case InstrCall, InstrReturnCall:
		ft, ok := v.funcType(in.Index)
		if !ok {
			return v.verr(ErrUnknownFunc, "")
		}
		if err := v.popAll(ft.Params); err != nil {
			return err
		}
		if buildfeatures.TailCall != 0 && in.Kind == InstrReturnCall {
			if !v.matchValTypes(ft.Results, v.ctrls[0].out) {
				return v.verr(ErrTypeMismatch, "return_call")
			}
			v.unreachable()
		} else {
			v.pushAll(ft.Results)
		}
	case InstrCallIndirect, InstrReturnCallIndirect:
		ft := v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
		if ft == nil {
			return v.verr(ErrUnknownType, "call_indirect")
		}
		tt, ok := v.tableType(in.Index2)
		if !ok {
			return v.verr(ErrUnknownTable, "")
		}
		if buildfeatures.ReferenceSignatures && !v.refSubtype(tt.Ref, AbsRef(HeapFunc)) {
			return v.verr(ErrTypeMismatch, "call_indirect table element type")
		}
		if err := v.popExpect(tableAddrType(tt)); err != nil {
			return err
		}
		if err := v.popAll(ft.Params); err != nil {
			return err
		}
		if buildfeatures.TailCall != 0 && in.Kind == InstrReturnCallIndirect {
			if !v.matchValTypes(ft.Results, v.ctrls[0].out) {
				return v.verr(ErrTypeMismatch, "return_call_indirect")
			}
			v.unreachable()
		} else {
			v.pushAll(ft.Results)
		}
	case InstrDrop:
		_, err := v.pop()
		return err
	case InstrSelect:
		// The typed opcode (0x1c) always has an extension payload, including when
		// its decoded result vector is empty. It requires exactly one value type;
		// only the extension-free 0x1b form is the implicit numeric/vector select.
		typedSelect := in.ext != nil
		if typedSelect && len(in.ValTypes()) != 1 {
			return v.verr(ErrTypeMismatch, "select type arity")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if typedSelect {
			if err := v.popExpect(in.ValTypes()[0]); err != nil {
				return err
			}
			if err := v.popExpect(in.ValTypes()[0]); err != nil {
				return err
			}
			v.push(in.ValTypes()[0])
		} else {
			a, err := v.pop()
			if err != nil {
				return err
			}
			b, err := v.pop()
			if err != nil {
				return err
			}
			// The implicit form is restricted to numeric and vector values. A
			// stack-polymorphic unknown still matches any permitted operand, but
			// must not hide a known reference operand.
			if (!a.unknown() && !isImplicitSelectType(a.t)) || (!b.unknown() && !isImplicitSelectType(b.t)) {
				return v.verr(ErrTypeMismatch, "implicit select operand type")
			}
			if !a.unknown() && !b.unknown() && !equalValType(a.t, b.t) {
				return v.verr(ErrTypeMismatch, "select")
			}
			if a.unknown() {
				v.vals = append(v.vals, b)
			} else {
				v.vals = append(v.vals, a)
			}
		}
	case InstrLocalGet, InstrLocalSet, InstrLocalTee:
		return v.stepLocalIndex(in.Kind, in.Index)
	case InstrGlobalGet:
		typ, mutable, ok := v.globalProperties(in.Index)
		if !ok {
			return v.verr(ErrUnknownGlobal, "")
		}
		if v.constOnly && (mutable || uint(in.Index) >= uint(v.constGlobalLimit) ||
			((buildfeatures.ExtendedConstExpressions == 0 || !v.features.ExtendedConstGlobals) && uint(in.Index) >= uint(len(v.importsOfKind(ExternGlobal))))) {
			return v.verr(ErrConstExprRequired, "global.get")
		}
		v.push(typ)
	case InstrGlobalSet:
		typ, mutable, ok := v.globalProperties(in.Index)
		if !ok {
			return v.verr(ErrUnknownGlobal, "")
		}
		if !mutable {
			return v.verr(ErrImmutableGlobal, "")
		}
		return v.popExpect(typ)
	case InstrTableGet, InstrTableSet, InstrRefNull, InstrRefFunc, InstrRefIsNull:
		if !buildfeatures.ReferenceSignatures {
			return v.verr(ErrUnsupportedFeature, "reference-types is disabled in this build")
		}
		return v.stepReferenceInstruction(in)
	case InstrI32Const:
		v.push(I32)
	case InstrI64Const:
		v.push(I64)
	case InstrF32Const:
		v.push(F32)
	case InstrF64Const:
		v.push(F64)

	default:
		if handled, err := v.proposalStep(in); handled || err != nil {
			return err
		}
		return v.stackEffect(in)
	}
	return nil
}

func isConstInstruction(k InstrKind) bool {
	switch k {
	case InstrI32Const, InstrI64Const, InstrF32Const, InstrF64Const, InstrV128Const, InstrRefNull, InstrRefFunc, InstrGlobalGet,
		InstrStructNewDefault, InstrArrayNewFixed, InstrStringConst:
		return true
	case InstrI32Add, InstrI32Sub, InstrI32Mul, InstrI64Add, InstrI64Sub, InstrI64Mul:
		return buildfeatures.ExtendedConstArithmetic
	}
	return false
}
func isImplicitSelectType(t ValType) bool {
	return t.Kind() == ValNum || t.Kind() == ValVec
}

// matchValTypes reports whether actual values may flow to expected result
// positions. Tail calls use this covariant relation: a non-null/indexed reference
// result may satisfy a nullable/abstract caller result without requiring equality.
func (v *funcValidator) matchValTypes(actual, expected []ValType) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if !v.subtype(actual[i], expected[i]) {
			return false
		}
	}
	return true
}

func sameValTypes(a, b []ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalValType(a[i], b[i]) {
			return false
		}
	}
	return true
}

func (v *funcValidator) stackEffect(in *Instruction) error {
	k := in.Kind
	if e := scalarOpEffect(k); e.cat != effNone {
		a := e.a.valType()
		switch e.cat {
		case effUnary:
			if err := v.popExpect(a); err != nil {
				return err
			}
			v.push(a)
		case effBinary:
			if err := v.popExpect(a); err != nil {
				return err
			}
			if err := v.popExpect(a); err != nil {
				return err
			}
			v.push(a)
		case effCompare:
			if err := v.popExpect(a); err != nil {
				return err
			}
			if err := v.popExpect(a); err != nil {
				return err
			}
			v.push(I32)
		case effTest:
			if err := v.popExpect(a); err != nil {
				return err
			}
			v.push(I32)
		case effConv:
			if err := v.popExpect(a); err != nil {
				return err
			}
			v.push(e.b.valType())
		case effLoad:
			if err := v.checkMem(uint32(e.align)); err != nil {
				return err
			}
			if err := v.popExpect(I32); err != nil {
				return err
			}
			v.push(a)
		case effStore:
			if err := v.checkMem(uint32(e.align)); err != nil {
				return err
			}
			if err := v.popExpect(a); err != nil {
				return err
			}
			return v.popExpect(I32)
		}
		return nil
	}
	switch k {
	case InstrMemorySize:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		v.push(addr)
		return nil
	case InstrMemoryGrow:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(addr)
		return nil
	case InstrI32TruncSatF32S, InstrI32TruncSatF32U:
		if err := v.popExpect(F32); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrI32TruncSatF64S, InstrI32TruncSatF64U:
		if err := v.popExpect(F64); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrI64TruncSatF32S, InstrI64TruncSatF32U:
		if err := v.popExpect(F32); err != nil {
			return err
		}
		v.push(I64)
		return nil
	case InstrI64TruncSatF64S, InstrI64TruncSatF64U:
		if err := v.popExpect(F64); err != nil {
			return err
		}
		v.push(I64)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, k.String())
}
func (v *funcValidator) checkMem(align uint32) error {
	_, err := v.checkMemArg(MemArg{Align: align}, align)
	return err
}

func (v *funcValidator) checkDataIndex(idx uint32, op string) error {
	// Bulk-memory data instructions are guarded by the data count section. The
	// segment may have any mode; active segments are already dropped at runtime.
	if v.m.DataCount == nil || idx >= *v.m.DataCount || uint(idx) >= uint(len(v.m.Data)) {
		return v.verr(ErrInvalidDataCount, op+" data index")
	}
	return nil
}

func tableAddrType(tt TableType) ValType {
	if buildfeatures.Table64 != 0 && tt.Limits.Addr64 {
		return I64
	}
	return I32
}

func (v *funcValidator) tableAddrType(idx uint32) (ValType, TableType, error) {
	tt, ok := v.tableType(idx)
	if !ok {
		return ValType{}, TableType{}, v.verr(ErrUnknownTable, "")
	}
	return tableAddrType(tt), tt, nil
}

func (v *funcValidator) checkMemArg(ma MemArg, natural uint32) (ValType, error) {
	idx := uint32(0)
	if ma.Mem != nil {
		idx = uint32(*ma.Mem)
		if buildfeatures.MultiMemory == 0 && idx != 0 {
			return ValType{}, v.verr(ErrUnsupportedFeature, "multi-memory is disabled in this build")
		}
	}
	flags, ok := v.memoryProperties(idx)
	if !ok {
		return ValType{}, v.verr(ErrUnknownMemory, "")
	}
	if ma.Align > natural {
		return ValType{}, v.verr(ErrInvalidAlignment, "")
	}
	if buildfeatures.Memory64 != 0 && flags&externTypeAddr64 != 0 {
		return I64, nil
	}
	if ma.Offset > uint64(^uint32(0)) {
		return ValType{}, v.verr(ErrInvalidAlignment, "offset out of range for i32 memory")
	}
	return I32, nil
}

func (v *funcValidator) checkAtomicMemArg(ma MemArg, natural uint32) (ValType, error) {
	addr, err := v.checkMemArg(ma, natural)
	if err != nil {
		return ValType{}, err
	}
	if ma.Align != natural {
		return ValType{}, v.verr(ErrInvalidAlignment, "atomic memory instruction requires natural alignment")
	}
	return addr, nil
}

// opEffect is a precomputed stack effect for the simple numeric/mem instructions,
// collapsing the per-instruction cascade of map lookups (unary → binaryOps →
// compare → test → conversions → loads → stores) into one array index — the
// hottest map cost in validation. Built once at init from those maps, which stay
// the single source of truth.
type opEffectCat uint8

const (
	effNone    opEffectCat = iota
	effUnary               // pop a; push a
	effBinary              // pop a; pop a; push a
	effCompare             // pop a; pop a; push i32
	effTest                // pop a; push i32
	effConv                // pop a; push b
	effLoad                // checkMem(align); pop i32; push a
	effStore               // checkMem(align); pop a; pop i32
)

// effectValue is the compact scalar/vector subset used by precomputed validator
// effects. Storing a full ValType here made each table entry carry the complete
// reference-type descriptor even though numeric and SIMD instructions only use
// five primitive values.
type effectValue uint8

const (
	effectNone effectValue = iota
	effectI32
	effectI64
	effectF32
	effectF64
	effectV128
)

var effectValTypes = [...]ValType{
	effectNone: {},
	effectI32:  {lo: i32ValueBits},
	effectI64:  {lo: i64ValueBits},
	effectF32:  {lo: f32ValueBits},
	effectF64:  {lo: f64ValueBits},
	effectV128: {lo: v128ValueBits},
}

func (t effectValue) valType() ValType { return effectValTypes[t] }

type opEffect struct {
	cat   opEffectCat
	a, b  effectValue
	align uint8
}

// Sign extension is the last scalar instruction family in kind order.
var opEffects [int(InstrI32Extend8S) + signExtensionBuilt*int(InstrI64Extend32S-InstrI32Extend8S+1)]opEffect
var scalarEffectsOnce sync.Once

func scalarOpEffect(kind InstrKind) opEffect {
	if kind < InstrKind(len(opEffects)) {
		return opEffects[kind]
	}
	return opEffect{}
}

func setOpEffectRange(first, last InstrKind, effect opEffect) {
	for kind := first; kind <= last; kind++ {
		opEffects[kind] = effect
	}
}

func setOpEffects(effect opEffect, kinds ...InstrKind) {
	for _, kind := range kinds {
		opEffects[kind] = effect
	}
}

func fillScalarEffects() {
	setOpEffects(opEffect{cat: effTest, a: effectI32}, InstrI32Eqz)
	setOpEffects(opEffect{cat: effTest, a: effectI64}, InstrI64Eqz)
	setOpEffectRange(InstrI32Eq, InstrI32GeU, opEffect{cat: effCompare, a: effectI32})
	setOpEffectRange(InstrI64Eq, InstrI64GeU, opEffect{cat: effCompare, a: effectI64})
	setOpEffectRange(InstrF32Eq, InstrF32Ge, opEffect{cat: effCompare, a: effectF32})
	setOpEffectRange(InstrF64Eq, InstrF64Ge, opEffect{cat: effCompare, a: effectF64})

	setOpEffectRange(InstrI32Clz, InstrI32Popcnt, opEffect{cat: effUnary, a: effectI32})
	setOpEffectRange(InstrI64Clz, InstrI64Popcnt, opEffect{cat: effUnary, a: effectI64})
	setOpEffectRange(InstrF32Abs, InstrF32Sqrt, opEffect{cat: effUnary, a: effectF32})
	setOpEffectRange(InstrF64Abs, InstrF64Sqrt, opEffect{cat: effUnary, a: effectF64})
	if buildfeatures.SignExtensionOps != 0 {
		setOpEffectRange(InstrI32Extend8S, InstrI32Extend16S, opEffect{cat: effUnary, a: effectI32})
		setOpEffectRange(InstrI64Extend8S, InstrI64Extend32S, opEffect{cat: effUnary, a: effectI64})
	}

	setOpEffectRange(InstrI32Add, InstrI32Rotr, opEffect{cat: effBinary, a: effectI32})
	setOpEffectRange(InstrI64Add, InstrI64Rotr, opEffect{cat: effBinary, a: effectI64})
	setOpEffectRange(InstrF32Add, InstrF32Copysign, opEffect{cat: effBinary, a: effectF32})
	setOpEffectRange(InstrF64Add, InstrF64Copysign, opEffect{cat: effBinary, a: effectF64})

	setOpEffects(opEffect{cat: effConv, a: effectI64, b: effectI32}, InstrI32WrapI64)
	setOpEffects(opEffect{cat: effConv, a: effectF32, b: effectI32}, InstrI32TruncF32S, InstrI32TruncF32U, InstrI32ReinterpretF32)
	setOpEffects(opEffect{cat: effConv, a: effectF64, b: effectI32}, InstrI32TruncF64S, InstrI32TruncF64U)
	setOpEffects(opEffect{cat: effConv, a: effectI32, b: effectI64}, InstrI64ExtendI32S, InstrI64ExtendI32U)
	setOpEffects(opEffect{cat: effConv, a: effectF32, b: effectI64}, InstrI64TruncF32S, InstrI64TruncF32U)
	setOpEffects(opEffect{cat: effConv, a: effectF64, b: effectI64}, InstrI64TruncF64S, InstrI64TruncF64U, InstrI64ReinterpretF64)
	setOpEffects(opEffect{cat: effConv, a: effectI32, b: effectF32}, InstrF32ConvertI32S, InstrF32ConvertI32U, InstrF32ReinterpretI32)
	setOpEffects(opEffect{cat: effConv, a: effectI64, b: effectF32}, InstrF32ConvertI64S, InstrF32ConvertI64U)
	setOpEffects(opEffect{cat: effConv, a: effectF64, b: effectF32}, InstrF32DemoteF64)
	setOpEffects(opEffect{cat: effConv, a: effectI32, b: effectF64}, InstrF64ConvertI32S, InstrF64ConvertI32U)
	setOpEffects(opEffect{cat: effConv, a: effectI64, b: effectF64}, InstrF64ConvertI64S, InstrF64ConvertI64U, InstrF64ReinterpretI64)
	setOpEffects(opEffect{cat: effConv, a: effectF32, b: effectF64}, InstrF64PromoteF32)

	setOpEffects(opEffect{cat: effLoad, a: effectI32, align: 2}, InstrI32Load)
	setOpEffects(opEffect{cat: effLoad, a: effectI64, align: 3}, InstrI64Load)
	setOpEffects(opEffect{cat: effLoad, a: effectF32, align: 2}, InstrF32Load)
	setOpEffects(opEffect{cat: effLoad, a: effectF64, align: 3}, InstrF64Load)
	setOpEffects(opEffect{cat: effLoad, a: effectI32}, InstrI32Load8S, InstrI32Load8U)
	setOpEffects(opEffect{cat: effLoad, a: effectI32, align: 1}, InstrI32Load16S, InstrI32Load16U)
	setOpEffects(opEffect{cat: effLoad, a: effectI64}, InstrI64Load8S, InstrI64Load8U)
	setOpEffects(opEffect{cat: effLoad, a: effectI64, align: 1}, InstrI64Load16S, InstrI64Load16U)
	setOpEffects(opEffect{cat: effLoad, a: effectI64, align: 2}, InstrI64Load32S, InstrI64Load32U)

	setOpEffects(opEffect{cat: effStore, a: effectI32, align: 2}, InstrI32Store)
	setOpEffects(opEffect{cat: effStore, a: effectI64, align: 3}, InstrI64Store)
	setOpEffects(opEffect{cat: effStore, a: effectF32, align: 2}, InstrF32Store)
	setOpEffects(opEffect{cat: effStore, a: effectF64, align: 3}, InstrF64Store)
	setOpEffects(opEffect{cat: effStore, a: effectI32}, InstrI32Store8)
	setOpEffects(opEffect{cat: effStore, a: effectI32, align: 1}, InstrI32Store16)
	setOpEffects(opEffect{cat: effStore, a: effectI64}, InstrI64Store8)
	setOpEffects(opEffect{cat: effStore, a: effectI64, align: 1}, InstrI64Store16)
	setOpEffects(opEffect{cat: effStore, a: effectI64, align: 2}, InstrI64Store32)
}

func (v *funcValidator) stepLocalIndex(kind InstrKind, index uint32) error {
	typ, ok := v.localType(index)
	if !ok {
		return v.verr(ErrUnknownLocal, "")
	}
	if kind == InstrLocalGet {
		if !v.localIsInitialized(index, typ) {
			return v.verr(ErrUninitializedLocal, "")
		}
		v.push(typ)
		return nil
	}
	if err := v.popExpect(typ); err != nil {
		return err
	}
	v.initializeLocal(index, typ)
	if kind == InstrLocalTee {
		v.push(typ)
	}
	return nil
}

// stepBranch preserves the operand/error order shared by decoded and raw-body
// validation without materializing an Instruction for each scalar branch.
func (v *funcValidator) stepBranch(kind InstrKind, index uint32) error {
	if kind == InstrBrIf {
		if err := v.popExpect(I32); err != nil {
			return err
		}
	}
	types, err := v.label(index)
	if err != nil {
		return err
	}
	if err := v.popAll(types); err != nil {
		return err
	}
	if kind == InstrBr {
		v.unreachable()
	} else {
		v.pushAll(types)
	}
	return nil
}
