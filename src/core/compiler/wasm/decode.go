// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import "github.com/wago-org/wago/internal/buildfeatures"

const (
	secCustom    = 0
	secType      = 1
	secImport    = 2
	secFunction  = 3
	secTable     = 4
	secMemory    = 5
	secGlobal    = 6
	secExport    = 7
	secStart     = 8
	secElement   = 9
	secCode      = 10
	secData      = 11
	secDataCount = 12
	secTag       = 13
)

// Section order includes proposal sections by decode position, not numeric id:
// tag is decoded after memory and before globals, while data_count is decoded
// before code. IDs above 13 are reserved by the WebAssembly 2.0 core format.
var sectionOrder = [...]uint8{
	secType: 1, secImport: 2, secFunction: 3, secTable: 4, secMemory: 5,
	secTag: 6, secGlobal: 7, secExport: 8, secStart: 9,
	secElement: 10, secDataCount: 11, secCode: 12, secData: 13,
}

func lookupSectionOrder(id byte) (uint8, bool) {
	if int(id) >= len(sectionOrder) {
		return 0, false
	}
	order := sectionOrder[id]
	return order, order != 0
}

// DecodeModule decodes a WebAssembly binary into the compact module
// representation used by validation and lowering. Function bodies and const
// expressions retain their raw bytecode; DecodeModule does not materialize a
// structured function-body instruction tree. This syntax-only entry accepts
// Core 3 memory grammar; validation applies the selected feature policy.
func DecodeModule(data []byte) (*Module, error) {
	dm, err := DecodeModuleByteBacked(data)
	if err != nil {
		return nil, err
	}
	return dm.Module, nil
}

// DecodeModuleWithFeatures decodes using explicit feature-dependent wire grammar.
func DecodeModuleWithFeatures(data []byte, features ValidationFeatures) (*Module, error) {
	dm, err := DecodeModuleByteBackedWithFeatures(data, features)
	if err != nil {
		return nil, err
	}
	return dm.Module, nil
}

func decodeSection(m *Module, r *reader, id byte) error {
	switch id {
	case secCustom:
		name, err := r.name()
		if err != nil {
			return err
		}
		payload, err := r.bytes(r.left())
		if err != nil {
			return err
		}
		firstName := name == "name"
		if firstName {
			for _, custom := range m.Customs {
				if custom.Name == "name" {
					firstName = false
					break
				}
			}
		}
		if firstName {
			ns, err := decodeOptionalNameSec(payload, r.budget)
			if err != nil {
				return err
			}
			m.NameSec = ns
		}
		if err := r.reserve(uint64(len(payload)), 2); err != nil {
			return err
		}
		ownedPayload := append([]byte(nil), payload...)
		if firstName {
			m.RawNameSecPayload = ownedPayload
		}
		m.Customs = append(m.Customs, CustomSec{Name: name, Data: ownedPayload})
	case secType:
		v, err := decodeTypeSection(r)
		if err != nil {
			return err
		}
		markRecursiveTypeIndexes(v)
		m.Types = v
	case secImport:
		v, compact, err := decodeImports(r)
		if err != nil {
			return err
		}
		m.Imports = v
		m.UsesCompactImports = compact
	case secFunction:
		v, err := readVec(r, func(r *reader) (TypeIdx, error) { return decodeTypeIdx(r) })
		if err != nil {
			return err
		}
		m.FuncTypes = v
	case secMemory:
		v, err := readVec(r, decodeMemType)
		if err != nil {
			return err
		}
		m.Memories = v
	case secTag:
		if buildfeatures.ExceptionHandling == 0 {
			return disabledInstruction("exception-handling", r.off())
		}
		v, err := readVec(r, decodeTagType)
		if err != nil {
			return err
		}
		m.Tags = v
	case secExport:
		v, err := readVec(r, decodeExport)
		if err != nil {
			return err
		}
		m.Exports = v
	case secStart:
		i, err := r.u32()
		if err != nil {
			return err
		}
		m.Start = ptr(FuncIdx(i))
	case secDataCount:
		c, err := r.u32()
		if err != nil {
			return err
		}
		m.DataCount = &c
	default:
		return &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	return nil
}

func decodeNumType(r *reader) (NumType, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	switch NumType(b) {
	case NumI32, NumI64, NumF32, NumF64:
		return NumType(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeAbsHeapType(r *reader) (AbsHeapType, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	if b >= 0x69 && b <= 0x74 || b == 0x64 {
		return AbsHeapType(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeTypeIdx(r *reader) (TypeIdx, error) { x, err := r.u32(); return TypeIdx{Index: x}, err }
func decodeS33TypeIdx(r *reader) (TypeIdx, error) {
	x, err := r.s33()
	if err != nil {
		return TypeIdx{}, err
	}
	if x < 0 {
		return TypeIdx{}, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
	}
	return TypeIdx{Index: uint32(x)}, nil
}
func decodeHeapType(r *reader) (HeapType, error) {
	if b, ok := r.peek(); ok && (b == 0x64 || b >= 0x69 && b <= 0x74) {
		a, err := decodeAbsHeapType(r)
		return AbsHeap(a), err
	}
	idx, err := decodeS33TypeIdx(r)
	if err != nil {
		return HeapType{}, err
	}
	return IndexedHeap(idx), nil
}

func decodeRefHeapType(r *reader) (bool, HeapType, error) {
	if b, ok := r.peek(); ok && b == 0x62 {
		_, _ = r.byte()
		idx, err := decodeS33TypeIdx(r)
		if err != nil {
			return false, HeapType{}, err
		}
		return true, IndexedHeap(idx), nil
	}
	ht, err := decodeHeapType(r)
	return false, ht, err
}
func decodeRefType(r *reader) (RefType, error) {
	b, err := r.byte()
	if err != nil {
		return RefType{}, err
	}
	switch b {
	case 0x63:
		exact, ht, err := decodeRefHeapType(r)
		if err != nil {
			return RefType{}, err
		}
		// Preserve the explicit (ref null ...) encoding form. Bare is retained
		// only for binary round trips and must not affect semantic type identity.
		return Ref(true, ht, exact), nil
	case 0x64:
		exact, ht, err := decodeRefHeapType(r)
		if err != nil {
			return RefType{}, err
		}
		return Ref(false, ht, exact), nil
	default:
		if b == 0x64 || b >= 0x69 && b <= 0x74 {
			return AbsRef(AbsHeapType(b)), nil
		}
		return RefType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
}
func decodeValType(r *reader) (ValType, error) {
	b, ok := r.peek()
	if !ok {
		return ValType{}, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c:
		n, err := decodeNumType(r)
		return newValType(ValNum, n), err
	case 0x7b:
		if buildfeatures.SIMD == 0 {
			return ValType{}, disabledInstruction("simd", r.off())
		}
		_, _ = r.byte()
		return V128, nil
	case 0x63, 0x64, 0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69, 0x71, 0x72, 0x73, 0x74:
		if !buildfeatures.ReferenceSignatures {
			return ValType{}, disabledInstruction("reference-types", r.off())
		}
		rt, err := decodeRefType(r)
		return RefVal(rt), err
	default:
		return ValType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
	}
}

func decodeResultType(r *reader) ([]ValType, error) { return readVec(r, decodeValType) }
func decodeMut(r *reader) (Mut, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	if b == 0 || b == 1 {
		return Mut(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeStorageType(r *reader) (StorageType, error) {
	if b, ok := r.peek(); ok && (b == 0x77 || b == 0x78) {
		_, _ = r.byte()
		return StoragePacked(PackType(b)), nil
	}
	vt, err := decodeValType(r)
	return StorageVal(vt), err
}
func decodeFieldType(r *reader) (FieldType, error) {
	st, err := decodeStorageType(r)
	if err != nil {
		return FieldType{}, err
	}
	m, err := decodeMut(r)
	if err != nil {
		return FieldType{}, err
	}
	return NewFieldType(st, m), nil
}

func decodeCompType(r *reader) (CompType, error) {
	b, err := r.byte()
	if err != nil {
		return CompType{}, err
	}
	switch b {
	case 0x5e:
		if buildfeatures.GC == 0 {
			return CompType{}, disabledInstruction("gc", r.off()-1)
		}
		ft, err := decodeFieldType(r)
		return CompType{Kind: CompArray, Array: ft}, err
	case 0x5f:
		if buildfeatures.GC == 0 {
			return CompType{}, disabledInstruction("gc", r.off()-1)
		}
		f, err := readVec(r, decodeFieldType)
		return CompType{Kind: CompStruct, Fields: f}, err
	case 0x60:
		p, err := decodeResultType(r)
		if err != nil {
			return CompType{}, err
		}
		res, err := decodeResultType(r)
		return CompType{Kind: CompFunc, Params: p, Results: res}, err
	default:
		return CompType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
}
func decodeTypeMetadata(r *reader) (TypeMetadata, error) {
	var tm TypeMetadata
	for {
		b, ok := r.peek()
		if !ok || (b != 0x4c && b != 0x4d) {
			return tm, nil
		}
		_, _ = r.byte()
		idx, err := decodeTypeIdx(r)
		if err != nil {
			return tm, err
		}
		if b == 0x4c {
			if tm.Describes.Present() || tm.Descriptor.Present() {
				return tm, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			tm.Describes = SomeTypeIdx(idx)
		} else {
			if tm.Descriptor.Present() {
				return tm, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			tm.Descriptor = SomeTypeIdx(idx)
		}
	}
}
func decodeSubType(r *reader) (SubType, error) {
	if err := r.reserveType(); err != nil {
		return SubType{}, err
	}
	b, ok := r.peek()
	if !ok {
		return SubType{}, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	st := SubType{Final: true}
	if b == 0x4f || b == 0x50 {
		if buildfeatures.GC == 0 {
			return st, disabledInstruction("gc", r.off())
		}
		_, _ = r.byte()
		st.HasPrefix = true
		st.Final = b == 0x4f
		supers, err := readVec(r, decodeTypeIdx)
		if err != nil {
			return st, err
		}
		st.Supers = supers
	}
	if buildfeatures.GC != 0 {
		meta, err := decodeTypeMetadata(r)
		if err != nil {
			return st, err
		}
		st.Metadata = meta
	} else if b == 0x4c || b == 0x4d {
		return st, disabledInstruction("gc", r.off())
	}
	ct, err := decodeCompType(r)
	if err != nil {
		return st, err
	}
	st.Comp = ct
	return st, nil
}
func decodeRecType(r *reader) (RecType, error) {
	if b, ok := r.peek(); ok && b == 0x4e {
		_, _ = r.byte()
		sts, err := readMetadataVec(r, decodeSubType)
		return RecType{SubTypes: sts}, err
	}
	st, err := decodeSubType(r)
	if err != nil {
		return RecType{}, err
	}
	return RecType{SubTypes: []SubType{st}}, nil
}

func decodeLimits(r *reader) (Limits, error) {
	flag, err := r.byte()
	if err != nil {
		return Limits{}, err
	}
	l := Limits{}
	switch flag {
	case 0x00, 0x01:
		min, err := r.u32()
		if err != nil {
			return l, err
		}
		l.Min = uint64(min)
		if flag == 0x01 {
			max, err := r.u32()
			if err != nil {
				return l, err
			}
			l.Max, l.HasMax = uint64(max), true
		}
		return l, nil
	case 0x04, 0x05:
		if buildfeatures.Table64 == 0 {
			return l, disabledInstruction("table64", r.off()-1)
		}
		l.Addr64 = true
		min, err := r.u64()
		if err != nil {
			return l, err
		}
		l.Min = min
		if flag == 0x05 {
			max, err := r.u64()
			if err != nil {
				return l, err
			}
			l.Max, l.HasMax = max, true
		}
		return l, nil
	default:
		return l, &DecodeError{Code: ErrInvalidLimits, Offset: r.off() - 1}
	}
}
func decodeMemType(r *reader) (MemType, error) {
	flag, err := r.byte()
	if err != nil {
		return MemType{}, err
	}
	flagOffset := r.off() - 1
	mt := MemType{}
	switch flag {
	case 0, 1, 2, 3:
		if buildfeatures.Threads == 0 && flag >= 2 {
			return mt, disabledInstruction("threads", flagOffset)
		}
		mt.Shared = flag == 2 || flag == 3
		min, err := r.u32()
		if err != nil {
			return mt, err
		}
		mt.Limits.Min = uint64(min)
		if flag == 2 {
			return mt, &DecodeError{Code: ErrInvalidLimits, Offset: flagOffset}
		}
		if flag == 1 || flag == 3 {
			max, err := r.u32()
			if err != nil {
				return mt, err
			}
			mt.Limits.Max, mt.Limits.HasMax = uint64(max), true
		}
		return mt, nil
	case 4, 5, 6, 7:
		if buildfeatures.Memory64 == 0 {
			return mt, disabledInstruction("memory64", flagOffset)
		}
		if buildfeatures.Threads == 0 && flag >= 6 {
			return mt, disabledInstruction("threads", flagOffset)
		}
		mt.Shared = flag == 6 || flag == 7
		mt.Limits.Addr64 = true
		min, err := r.u64()
		if err != nil {
			return mt, err
		}
		mt.Limits.Min = min
		if flag == 6 {
			return mt, &DecodeError{Code: ErrInvalidLimits, Offset: flagOffset}
		}
		if flag == 5 || flag == 7 {
			max, err := r.u64()
			if err != nil {
				return mt, err
			}
			mt.Limits.Max, mt.Limits.HasMax = max, true
		}
		return mt, nil
	default:
		return mt, &DecodeError{Code: ErrInvalidLimits, Offset: r.off() - 1}
	}
}
func decodeTableType(r *reader) (TableType, error) {
	rt, err := decodeTableRefType(r)
	if err != nil {
		return TableType{}, err
	}
	lim, err := decodeLimits(r)
	return TableType{Ref: rt, Limits: lim}, err
}
func decodeTableRefType(r *reader) (RefType, error) {
	if buildfeatures.ReferenceSignatures {
		return decodeRefType(r)
	}
	b, err := r.byte()
	if err != nil {
		return RefType{}, err
	}
	switch {
	case b == byte(HeapFunc):
		return AbsRef(HeapFunc), nil
	case b == 0x63 || b == 0x64 || b >= 0x69 && b <= 0x74:
		return RefType{}, disabledInstruction("reference-types", r.off()-1)
	default:
		return RefType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
}

func decodeGlobalType(r *reader) (GlobalType, error) {
	vt, err := decodeValType(r)
	if err != nil {
		return GlobalType{}, err
	}
	m, err := decodeMut(r)
	if err != nil {
		return GlobalType{}, err
	}
	return GlobalType{Type: vt, Mutable: m == Var}, nil
}
func decodeTagType(r *reader) (TagType, error) {
	if buildfeatures.ExceptionHandling == 0 {
		return TagType{}, disabledInstruction("exception-handling", r.off())
	}
	b, err := r.byte()
	if err != nil {
		return TagType{}, err
	}
	if b != 0 {
		return TagType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
	idx, err := decodeTypeIdx(r)
	return TagType{Type: idx}, err
}
func decodeExternIdx(r *reader) (ExternIdx, error) {
	k, err := r.byte()
	if err != nil {
		return ExternIdx{}, err
	}
	idx, err := r.u32()
	if err != nil {
		return ExternIdx{}, err
	}
	if k > 4 {
		return ExternIdx{}, &DecodeError{Code: ErrInvalidExport, Offset: r.off()}
	}
	return ExternIdx{Kind: ExternKind(k), Index: idx}, nil
}
func decodeExport(r *reader) (Export, error) {
	nm, err := r.name()
	if err != nil {
		return Export{}, err
	}
	idx, err := decodeExternIdx(r)
	return Export{Name: nm, Index: idx}, err
}
func decodeLocals(r *reader) (Locals, error) {
	var total uint64
	runs, err := readVec(r, func(r *reader) (LocalRun, error) {
		c, err := r.u32()
		if err != nil {
			return LocalRun{}, err
		}
		vt, err := decodeValType(r)
		if err != nil {
			return LocalRun{}, err
		}
		if uint64(c) > uint64(^uint32(0))-total {
			return LocalRun{}, &DecodeError{Code: ErrInvalidModule, Offset: r.off()}
		}
		total += uint64(c)
		return LocalRun{Count: c, Type: vt}, nil
	})
	return Locals{Runs: runs}, err
}
