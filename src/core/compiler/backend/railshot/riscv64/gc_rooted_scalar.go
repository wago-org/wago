//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const maxRootedScalarArrayFixed = 254

// admitRV64GCRootedScalarModule is the first source-shape admission shared by
// generic GC programs instead of a byte-for-byte product fixture. It keeps the
// runtime boundary deliberately closed while admitting ordinary scalar control
// over bounded numeric scalar structs. Instruction admission remains the
// responsibility of the typed integer walker, and exact allocation roots are
// authenticated independently by validateGCFrameRootPlans.
func admitRV64GCRootedScalarModule(m *wasm.Module) bool {
	if m == nil || len(m.Code) == 0 || len(m.Code) > MaxFunctions || len(m.Types) == 0 || len(m.Types) > MaxFunctions || len(m.Exports) > MaxFunctions ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 ||
		len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 ||
		len(m.StringRefs) != 0 || m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() ||
		m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Exports {
		if m.Exports[i].Index.Kind != wasm.ExternFunc {
			return false
		}
	}
	hasAggregate := false
	flatTypes := 0
	for i := range m.Types {
		group := &m.Types[i]
		if len(group.SubTypes) != 1 {
			return false
		}
		sub := &group.SubTypes[0]
		if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 ||
			sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() {
			return false
		}
		switch sub.Comp.Kind {
		case wasm.CompStruct:
			if len(sub.Comp.Fields) == 0 || len(sub.Comp.Fields) > 255 {
				return false
			}
			for _, field := range sub.Comp.Fields {
				storage := field.Storage()
				if storage.Packed() {
					if storage.Pack() != wasm.PackI8 && storage.Pack() != wasm.PackI16 {
						return false
					}
				} else if !scalarType(storage.Val(), true) {
					return false
				}
			}
			hasAggregate = true
		case wasm.CompArray:
			storage := sub.Comp.Array.Storage()
			if storage.Packed() {
				if storage.Pack() != wasm.PackI8 && storage.Pack() != wasm.PackI16 {
					return false
				}
			} else if !scalarType(storage.Val(), true) {
				return false
			}
			hasAggregate = true
		case wasm.CompFunc:
			if len(sub.Comp.Params) > 256 || len(sub.Comp.Results) > 256 {
				return false
			}
			for _, values := range [][]wasm.ValType{sub.Comp.Params, sub.Comp.Results} {
				for _, typ := range values {
					if !scalarType(typ, true) {
						return false
					}
				}
			}
		default:
			return false
		}
		flatTypes++
	}
	if !hasAggregate || flatTypes > MaxFunctions || len(m.FuncTypes) != len(m.Code) {
		return false
	}
	for _, typ := range m.FuncTypes {
		if typ.Rec || int(typ.Index) >= flatTypes {
			return false
		}
		ft, ok := m.TypeFunc(typ.Index)
		if !ok || ft.Kind != wasm.CompFunc {
			return false
		}
	}
	return true
}

func admitRV64GCRootedScalarWire(data []byte) bool {
	if len(data) < 8 || len(data) > MaxModuleBytes || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	want := [...]byte{1, 3, 7, 10}
	r := wasm.ReaderFrom(data[8:])
	section := 0
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil || section >= len(want) || id != want[section] {
			return false
		}
		n, err := r.U32()
		if err != nil || n == 0 || uint64(n) > uint64(r.BytesLeft()) {
			return false
		}
		start, end := r.Offset(), r.Offset()+int(n)
		payload := data[8+start : 8+end]
		probe := wasm.ReaderFrom(payload)
		count, err := probe.U32()
		if err != nil || count == 0 {
			return false
		}
		// Public decoding flattens a one-element explicit rec group into the same
		// module shape as an implicit singleton. Preserve that wire distinction by
		// parsing entry boundaries instead of scanning raw bytes: 0x4e is also a
		// perfectly valid vector length (78) inside a struct or function type.
		if id == 1 && !admitRV64GCRootedScalarTypeWire(payload) {
			return false
		}
		if err := r.JumpTo(end); err != nil {
			return false
		}
		section++
	}
	if section != len(want) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRootedScalarModule(m)
}

// AdmitGCRootedScalarSource reports the bounded generic struct source witness.
// The decoded backend and root-plan authenticator independently recheck it.
func AdmitGCRootedScalarSource(data []byte) bool { return admitRV64GCRootedScalarWire(data) }

func admitRV64GCRootedScalarTypeWire(payload []byte) bool {
	r := wasm.ReaderFrom(payload)
	count, err := r.U32()
	if err != nil || count == 0 || count > MaxFunctions {
		return false
	}
	valueTypes := func() bool {
		n, err := r.U32()
		if err != nil || n > 256 {
			return false
		}
		for range n {
			typ, err := r.Byte()
			if err != nil || typ < 0x7c || typ > 0x7f {
				return false
			}
		}
		return true
	}
	for range count {
		form, err := r.Byte()
		if err != nil {
			return false
		}
		switch form {
		case 0x5f:
			fields, err := r.U32()
			if err != nil || fields == 0 || fields > 255 {
				return false
			}
			for range fields {
				storage, err := r.Byte()
				if err != nil || storage != byte(wasm.PackI8) && storage != byte(wasm.PackI16) && (storage < 0x7c || storage > 0x7f) {
					return false
				}
				mut, err := r.Byte()
				if err != nil || mut > 1 {
					return false
				}
			}
		case 0x60:
			if !valueTypes() || !valueTypes() {
				return false
			}
		case 0x5e:
			storage, err := r.Byte()
			if err != nil || storage != byte(wasm.PackI8) && storage != byte(wasm.PackI16) && (storage < 0x7c || storage > 0x7f) {
				return false
			}
			mut, err := r.Byte()
			if err != nil || mut > 1 {
				return false
			}
		default:
			// Includes the explicit rec-group marker 0x4e and subtype prefixes.
			return false
		}
	}
	return !r.HasNext()
}

func rootedScalarHelperSlots(m *wasm.Module) int {
	slots := 4 // struct.set is the largest non-allocation helper.
	if m == nil {
		return slots
	}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 || group.SubTypes[0].Comp.Kind != wasm.CompStruct {
			continue
		}
		slots = max(slots, len(group.SubTypes[0].Comp.Fields)+1)
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return 65
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(r, op, &imm); err != nil {
				return 65
			}
			if imm.Kind == wasm.InstrArrayNewFixed {
				if imm.Index2 > maxRootedScalarArrayFixed {
					return maxRootedScalarArrayFixed + 3
				}
				slots = max(slots, int(imm.Index2)+2)
			}
		}
	}
	return slots
}

func rootedScalarHelperProfile(m *wasm.Module) (structs, arrays, ok bool) {
	if m == nil {
		return false, false, false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		r := wasm.NewReader(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false, false, false
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(r, op, &imm); err != nil {
				return false, false, false
			}
			switch imm.Kind {
			case wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrStructGet,
				wasm.InstrStructGetS, wasm.InstrStructGetU, wasm.InstrStructSet:
				structs = true
			case wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed,
				wasm.InstrArrayGet, wasm.InstrArrayGetS, wasm.InstrArrayGetU,
				wasm.InstrArraySet, wasm.InstrArrayLen, wasm.InstrArrayFill,
				wasm.InstrArrayCopy, wasm.InstrArrayInitData, wasm.InstrArrayInitElem,
				wasm.InstrArrayNewData, wasm.InstrArrayNewElem:
				arrays = true
			}
		}
	}
	return structs, arrays, true
}
