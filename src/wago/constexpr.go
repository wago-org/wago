// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// constExprInit is the internal reusable form for MVP const expressions whose
// literal bits are known at compile time unless an imported immutable global must
// be read later, after import values are supplied during instantiation.
type constExprInit struct {
	Bits        uint64
	V128        V128
	GlobalIndex int
	FuncIndex   int
	Expr        []byte
}

func (i constExprInit) GlobalRef() (int, bool) { return i.GlobalIndex, i.GlobalIndex >= 0 }
func (i constExprInit) FuncRef() (int, bool)   { return i.FuncIndex, i.FuncIndex >= 0 }

type constExprCompileContext struct {
	module    *wasm.Module
	types     []DefinedTypeDescriptor
	converter wasmTypeDescriptorConverter
}

func newConstExprCompileContext(m *wasm.Module) (*constExprCompileContext, error) {
	if m == nil {
		return nil, nil
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		return nil, err
	}
	return &constExprCompileContext{module: m, types: types, converter: newWasmTypeDescriptorConverter(m)}, nil
}

type constExprResult struct {
	bits        uint64
	v128        V128
	vtype       wasm.ValType
	GlobalIndex int
	FuncIndex   int
	Expr        []byte
}

func (r constExprResult) Init() constExprInit {
	return constExprInit{Bits: r.bits, V128: r.v128, GlobalIndex: r.GlobalIndex, FuncIndex: r.FuncIndex, Expr: r.Expr}
}

func applyGlobalInit(g *GlobalDef, init constExprInit) {
	g.Bits = init.Bits
	g.V128 = init.V128
	if idx, ok := init.GlobalRef(); ok {
		g.HasInitGlobal = true
		g.InitGlobal = idx
	}
	if idx, ok := init.FuncRef(); ok {
		g.HasInitFunc = true
		g.InitFunc = uint32(idx)
	}
	g.InitExpr = append([]byte(nil), init.Expr...)
}

func applyOffsetInit(o *OffsetInit, init constExprInit) {
	o.Base = uint32(init.Bits)
	if idx, ok := init.GlobalRef(); ok {
		o.HasGlobal = true
		o.Global = idx
	}
	o.Expr = append([]byte(nil), init.Expr...)
}

func applyElemOffset(e *ElemInit, init constExprInit) { applyOffsetInit(&e.Offset, init) }
func applyDataOffset(d *DataInit, init constExprInit) { applyOffsetInit(&d.Offset, init) }

func evalConstExpr(b []byte, want wasm.ValType) (uint64, error) {
	res, err := evalConstExprBytes(b, want)
	return res.bits, err
}

func evalConstExprBytes(b []byte, want wasm.ValType) (constExprResult, error) {
	return evalConstExprBytesWithModule(b, want, nil)
}

func evalConstExprBytesWithModule(b []byte, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		return constExprResult{}, err
	}
	return evalConstExprBytesWithContext(b, want, ctx)
}

func evalConstExprBytesWithContext(b []byte, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	if wasmGCBuilt {
		if got, matched, err := evalNullExternConversionConstExpr(b, want); matched {
			return got, err
		}
		if isI31RefType(want) || isAnyRefType(want) {
			if got, matched, err := evalI31ConstExprBytes(b, want, ctx); matched && err == nil {
				return got, nil
			}
		}
		if want.Kind() == wasm.ValRef && m != nil && len(b) != 0 && b[0] == 0xfb {
			return constExprResult{vtype: want, GlobalIndex: -1, FuncIndex: -1, Expr: append([]byte(nil), b...)}, nil
		}
	}
	r := wasm.ReaderFrom(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, err
	}
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch op {
	case 0x23: // global.get (valid in const expressions only for imported immutable globals)
		x, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(x)
		if !ok || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", x)
		}
		got.bits, got.vtype = 0, gt.Type
		got.GlobalIndex = int(x)
	case 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(uint32(v)), wasm.I32
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(v), wasm.I64
	case 0x43: // f32.const
		bb, err := r.Bytes(4)
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(binary.LittleEndian.Uint32(bb)), wasm.F32
	case 0x44: // f64.const
		bb, err := r.Bytes(8)
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = binary.LittleEndian.Uint64(bb), wasm.F64
	case 0xd0: // ref.null
		if !referenceSignaturesBuilt {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureReferenceTypes)
		}
		got.vtype, err = readConstRefNullType(&r, m)
		if err != nil {
			return constExprResult{}, err
		}
	case 0xd2: // ref.func
		if !referenceSignaturesBuilt {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureReferenceTypes)
		}
		idx, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		got.vtype, got.FuncIndex = constRefFuncType(m, idx), int(idx)
	case 0xfd: // v128.const
		if buildfeatures.SIMD == 0 {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureSIMD)
		}
		payload, err := readV128ConstExpr(&r)
		if err != nil {
			return constExprResult{}, err
		}
		copy(got.v128[:], payload)
		got.vtype = wasm.V128
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, fmt.Errorf("const expression missing end: %w", err)
	}
	if end != 0x0B {
		if wasmGCBuilt && want.Kind() == wasm.ValRef && m != nil {
			// Validation has already type-checked the complete Core 3 constant
			// expression. Preserve object-building programs for collector-backed
			// evaluation after preceding globals and function descriptors exist.
			return constExprResult{vtype: want, GlobalIndex: -1, FuncIndex: -1, Expr: append([]byte(nil), b...)}, nil
		}
		if !buildfeatures.ExtendedConstArithmetic {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureExtendedConst)
		}
		bits, usesGlobal, err := evalScalarConstExprProgram(b, want, moduleConstExprGlobalResolver(m))
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = bits, want
		got.GlobalIndex, got.FuncIndex = -1, -1
		if usesGlobal {
			got.Expr = append([]byte(nil), b...)
		}
		return got, nil
	}
	if r.BytesLeft() != 0 {
		return constExprResult{}, fmt.Errorf("const expression has trailing bytes")
	}
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype.String(), want.String())
	}
	return got, nil
}

func evalNullExternConversionConstExpr(b []byte, want wasm.ValType) (constExprResult, bool, error) {
	r := wasm.ReaderFrom(b)
	op, err := r.Byte()
	if err != nil || op != 0xd0 {
		return constExprResult{}, false, nil
	}
	_, heap, err := r.RefHeapType()
	if err != nil {
		return constExprResult{}, true, err
	}
	// A null converted any number of times between the any and extern
	// hierarchies is still null; the last conversion decides the type.
	// Validation has already proved each step well typed, but keep the
	// hierarchy check so a malformed chain still fails closed.
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	inExtern := heap == -17 || heap == -14 // extern / noextern
	conversions := 0
	for {
		if r.BytesLeft() == 0 {
			break
		}
		if b[len(b)-r.BytesLeft()] != 0xfb {
			break
		}
		if _, err := r.Byte(); err != nil {
			return constExprResult{}, true, err
		}
		sub, err := r.U32()
		if err != nil {
			return constExprResult{}, true, err
		}
		switch sub {
		case 26: // any.convert_extern
			if !inExtern {
				return constExprResult{}, true, fmt.Errorf("any.convert_extern constant source heap %d is not extern", heap)
			}
			got.vtype, inExtern = wasm.AnyRef, false
		case 27: // extern.convert_any
			if inExtern {
				return constExprResult{}, true, fmt.Errorf("extern.convert_any constant source heap %d is not any", heap)
			}
			got.vtype, inExtern = wasm.ExternRef, true
		default:
			if conversions == 0 {
				return constExprResult{}, false, nil
			}
			return constExprResult{}, true, fmt.Errorf("GC conversion constant expression has trailing instructions")
		}
		conversions++
	}
	if conversions == 0 {
		return constExprResult{}, false, nil
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, fmt.Errorf("GC conversion constant expression missing end: %w", err)
	}
	if end != 0x0b || r.BytesLeft() != 0 {
		return constExprResult{}, true, fmt.Errorf("GC conversion constant expression has trailing instructions")
	}
	if !constExprTypeMatches(got.vtype, want, nil) {
		return constExprResult{}, true, fmt.Errorf("const expression type %s, want %s", got.vtype.String(), want.String())
	}
	return got, true, nil
}

func isI31RefType(t wasm.ValType) bool {
	return t.Kind() == wasm.ValRef && t.Ref().Heap().Kind() == wasm.HeapAbs && t.Ref().Heap().Abs() == wasm.HeapI31
}

func isAnyRefType(t wasm.ValType) bool {
	return t.Kind() == wasm.ValRef && t.Ref().Heap().Kind() == wasm.HeapAbs && t.Ref().Heap().Abs() == wasm.HeapAny
}

func constExprTypeMatches(actual, required wasm.ValType, ctx *constExprCompileContext) bool {
	if valTypeEqual(actual, required) {
		return true
	}
	return referenceSignaturesBuilt && referenceConstExprTypeMatches(actual, required, ctx)
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.ValidateModule checks const-expression shape/type rules before compile
// reaches here, while this helper converts the supported MVP operators into
// instantiate-time bits or deferred imported-global references.
func evalConstExprWithModule(e wasm.Expr, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		return constExprResult{}, err
	}
	return evalConstExprWithContext(e, want, ctx)
}

func evalConstExprWithContext(e wasm.Expr, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return evalConstExprBytesWithContext(e.BodyBytes, want, ctx)
	}
	if len(e.Instrs) != 1 {
		encoded, err := wasm.EncodeExpr(e)
		if err != nil {
			return constExprResult{}, fmt.Errorf("encode const expression: %w", err)
		}
		return evalConstExprBytesWithContext(encoded, want, ctx)
	}
	in := e.Instrs[0]
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch in.Kind {
	case wasm.InstrI32Const:
		got.bits, got.vtype = uint64(uint32(in.I32)), wasm.I32
	case wasm.InstrI64Const:
		got.bits, got.vtype = uint64(in.I64), wasm.I64
	case wasm.InstrF32Const:
		got.bits, got.vtype = uint64(in.F32Bits), wasm.F32
	case wasm.InstrF64Const:
		got.bits, got.vtype = in.F64Bits, wasm.F64
	case wasm.InstrV128Const:
		if buildfeatures.SIMD == 0 {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureSIMD)
		}
		lanes := in.Lanes()
		for i, b := range lanes {
			got.v128[i] = byte(b)
		}
		got.vtype = wasm.V128
	case wasm.InstrRefNull:
		if !referenceSignaturesBuilt {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureReferenceTypes)
		}
		refType, err := constRefNullASTType(in, ctx)
		if err != nil {
			return constExprResult{}, err
		}
		got.vtype = refType
	case wasm.InstrRefFunc:
		if !referenceSignaturesBuilt {
			return constExprResult{}, checkCompiledFeatures(CoreFeatureReferenceTypes)
		}
		got.vtype, got.FuncIndex = constRefFuncType(m, in.Index), int(in.Index)
	case wasm.InstrGlobalGet:
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(in.Index)
		if !ok || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", in.Index)
		}
		got.bits, got.vtype = 0, gt.Type
		got.GlobalIndex = int(in.Index)
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode %s", in.Kind.String())
	}
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype.String(), want.String())
	}
	return got, nil
}

type constExprGlobalResolver func(uint32) (bits uint64, typ wasm.ValType, mutable bool, ok bool)

func moduleConstExprGlobalResolver(m *wasm.Module) constExprGlobalResolver {
	if m == nil {
		return nil
	}
	return func(index uint32) (uint64, wasm.ValType, bool, bool) {
		gt, ok := m.GlobalTypeByIndex(index)
		if !ok {
			return 0, wasm.ValType{}, false, false
		}
		return 0, gt.Type, gt.Mutable, true
	}
}

type scalarConstValue struct {
	bits uint64
	typ  wasm.ValType
}

// readScalarConstOperand handles the single-instruction forms shared by core
// offsets and the extended constant-expression stack evaluator.
func readScalarConstOperand(r *wasm.Reader, op byte, resolve constExprGlobalResolver) (scalarConstValue, error) {
	switch {
	case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23:
		index, err := r.U32()
		if err != nil {
			return scalarConstValue{}, err
		}
		if resolve == nil {
			return scalarConstValue{}, fmt.Errorf("extended const expression global.get %d has no resolver", index)
		}
		value, typ, mutable, ok := resolve(index)
		if !ok {
			return scalarConstValue{}, fmt.Errorf("extended const expression global.get %d is unavailable", index)
		}
		if mutable {
			return scalarConstValue{}, fmt.Errorf("extended const expression global.get %d is mutable", index)
		}
		if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) {
			return scalarConstValue{}, fmt.Errorf("extended const expression global.get %d has type %s", index, typ.String())
		}
		return scalarConstValue{bits: value, typ: typ}, nil
	case constExprNumericOpcodesForImage&(1<<(0x41-0x40)) != 0 && op == 0x41:
		value, err := r.I32()
		return scalarConstValue{bits: uint64(uint32(value)), typ: wasm.I32}, err
	case constExprNumericOpcodesForImage&(1<<(0x42-0x40)) != 0 && op == 0x42:
		value, err := r.I64()
		return scalarConstValue{bits: uint64(value), typ: wasm.I64}, err
	default:
		return scalarConstValue{}, fmt.Errorf("unsupported extended const expression opcode 0x%02x", op)
	}
}

func evalCompiledIntegerOperand(b []byte, want wasm.ValType, resolve constExprGlobalResolver) (uint64, error) {
	if buildfeatures.ExtendedConstArithmetic {
		bits, _, err := evalScalarConstExprProgram(b, want, resolve)
		return bits, err
	}
	// Wide offsets retain a literal or global.get as a wire expression. An
	// i31 initializer in a GC build can also have one of these operands.
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return 0, err
	}
	value, err := readScalarConstOperand(r, op, resolve)
	if err != nil {
		return 0, err
	}
	end, err := r.Byte()
	if err != nil {
		return 0, fmt.Errorf("const expression missing end: %w", err)
	}
	if end != 0x0b {
		return 0, checkCompiledFeatures(CoreFeatureExtendedConst)
	}
	if r.BytesLeft() != 0 {
		return 0, fmt.Errorf("const expression has trailing bytes")
	}
	if !wasm.EqualValType(value.typ, want) {
		return 0, fmt.Errorf("const expression type %s, want %s", value.typ.String(), want.String())
	}
	return value.bits, nil
}

func wasmScalarValType(t ValType) (wasm.ValType, bool) {
	switch t {
	case ValI32:
		return wasm.I32, true
	case ValI64:
		return wasm.I64, true
	default:
		return wasm.ValType{}, false
	}
}

type constExprContext uint8

const (
	constExprGlobalInitializer constExprContext = iota
	constExprDataOffset
	constExprElementOffset
)

type constExprGlobalScope struct {
	context constExprContext
	limit   int // exclusive absolute global index visible in this expression context
}

func validateCompiledScalarConstExpr(b []byte, want ValType, defs []GlobalDef, scope constExprGlobalScope) error {
	if want == ValI31Ref {
		if !wasmGCBuilt {
			return checkCompiledFeatures(CoreFeatureGC)
		}
		if gcConstOpcodesForImage&(1<<28) == 0 {
			return fmt.Errorf("i31 initializer missing ref.i31")
		}
		_, err := evalCompiledI31ConstExpr(b, nil, defs, scope)
		return err
	}
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(defs) {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		return 0, typ, defs[i].Mutable, true
	}
	_, err := evalCompiledIntegerOperand(b, wasmWant, resolve)
	return err
}

func evalCompiledScalarConstExpr(b []byte, want ValType, globals []*Global, defs []GlobalDef, scope constExprGlobalScope) (uint64, error) {
	if want == ValI31Ref {
		if !wasmGCBuilt {
			return 0, checkCompiledFeatures(CoreFeatureGC)
		}
		if gcConstOpcodesForImage&(1<<28) == 0 {
			return 0, fmt.Errorf("i31 initializer missing ref.i31")
		}
		return evalCompiledI31ConstExpr(b, globals, defs, scope)
	}
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return 0, fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(globals) || i >= len(defs) || globals[i] == nil {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		return readGlobalObject(globals[i], defs[i].Type), typ, defs[i].Mutable, true
	}
	return evalCompiledIntegerOperand(b, wasmWant, resolve)
}
