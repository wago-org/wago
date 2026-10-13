//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// admitRV64GCRootedScalarModule is the first source-shape admission shared by
// generic GC programs instead of a byte-for-byte product fixture. It keeps the
// runtime boundary deliberately closed while admitting ordinary scalar control
// over one-field i32 structs. Instruction admission remains the
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
	hasStruct := false
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
			if len(sub.Comp.Fields) != 1 || sub.Comp.Fields[0].Storage() != wasm.StorageVal(wasm.I32) {
				return false
			}
			hasStruct = true
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
	if !hasStruct || flatTypes > MaxFunctions || len(m.FuncTypes) != len(m.Code) {
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
		// The bounded profile has only numeric function types and i32
		// structs, so 0x4e cannot occur in a legal type payload. Rejecting it here
		// preserves the explicit-rec-group distinction lost by public decoding.
		if id == 1 && bytes.IndexByte(payload[probe.Offset():], 0x4e) >= 0 {
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
