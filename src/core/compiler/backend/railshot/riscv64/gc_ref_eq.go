//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCRefEqSourceDigest = [sha256.Size]byte{0x46, 0xb2, 0xbd, 0x3e, 0x45, 0x97, 0xba, 0x5a, 0x87, 0x14, 0x72, 0xaa, 0x14, 0xf5, 0x77, 0x7d, 0xf1, 0x8b, 0x72, 0x2b, 0x7f, 0x32, 0x83, 0xba, 0x1f, 0xc9, 0x46, 0xf4, 0x79, 0x1a, 0x3a, 0xdb}

func admitRV64GCRefEqWire(data []byte) bool {
	return len(data) == 197 && sha256.Sum256(data) == rv64GCRefEqSourceDigest
}

// AdmitGCRefEqSource is the wire-provenance half of the exact allocating
// gc/ref_eq checkpoint. Decoded admission independently proves the complete
// module before helper lowering is selected.
func AdmitGCRefEqSource(data []byte) bool { return admitRV64GCRefEqWire(data) }

func admitRV64GCRefEq(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 9 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Tables) != 1 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	table := m.Tables[0]
	if table.Init != nil || table.Type.Limits.Addr64 || table.Type.Limits.HasMax || table.Type.Limits.Min != 20 || !wasm.EqualValType(wasm.RefVal(table.Type.Ref), wasm.EqRef) {
		return false
	}
	wantSupers := [][]wasm.TypeIdx{nil, nil, nil, {{Index: 0}}, {{Index: 0}}, {{Index: 1}}, {{Index: 1}}, nil, nil}
	for i := range m.Types {
		group := &m.Types[i]
		if len(group.SubTypes) != 1 {
			return false
		}
		sub := &group.SubTypes[0]
		if sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || len(sub.Supers) != len(wantSupers[i]) {
			return false
		}
		for j := range sub.Supers {
			if sub.Supers[j] != wantSupers[i][j] {
				return false
			}
		}
		switch i {
		case 0, 3, 4:
			if sub.Final || !sub.HasPrefix || sub.Comp.Kind != wasm.CompStruct || len(sub.Comp.Fields) != 0 {
				return false
			}
		case 1, 5, 6:
			if sub.Final || !sub.HasPrefix || sub.Comp.Kind != wasm.CompStruct || len(sub.Comp.Fields) != 1 || sub.Comp.Fields[0] != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const) {
				return false
			}
		case 2:
			if !sub.Final || sub.HasPrefix || sub.Comp.Kind != wasm.CompArray || sub.Comp.Array != wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Const) {
				return false
			}
		case 7:
			if !i31CoreFunctionType(group, nil, nil) {
				return false
			}
		case 8:
			if !i31CoreFunctionType(group, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}) {
				return false
			}
		}
	}
	if m.FuncTypes[0] != (wasm.TypeIdx{Index: 7}) || m.FuncTypes[1] != (wasm.TypeIdx{Index: 8}) {
		return false
	}
	wantBodies := [][]byte{
		{0x41, 0, 0xd0, 0x6d, 0x26, 0, 0x41, 1, 0xd0, 0x6c, 0x26, 0, 0x41, 2, 0x41, 7, 0xfb, 0x1c, 0x26, 0, 0x41, 3, 0x41, 7, 0xfb, 0x1c, 0x26, 0, 0x41, 4, 0x41, 8, 0xfb, 0x1c, 0x26, 0, 0x41, 5, 0xfb, 1, 0, 0x26, 0, 0x41, 6, 0xfb, 1, 0, 0x26, 0, 0x41, 7, 0x41, 0, 0xfb, 7, 2, 0x26, 0, 0x41, 8, 0x41, 0, 0xfb, 7, 2, 0x26, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0x20, 1, 0x25, 0, 0xd3, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	return m.Exports[0].Name == "init" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 &&
		m.Exports[1].Name == "eq" && m.Exports[1].Index.Kind == wasm.ExternFunc && m.Exports[1].Index.Index == 1
}
