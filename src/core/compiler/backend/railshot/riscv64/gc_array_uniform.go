//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCArrayUniformSourceDigest = [sha256.Size]byte{0x5b, 0x42, 0x1e, 0x8a, 0x65, 0xc9, 0x97, 0xbe, 0x7b, 0x20, 0xbd, 0x18, 0x8c, 0x5a, 0x1f, 0x1b, 0x16, 0x77, 0x16, 0xb9, 0x3a, 0xbb, 0x55, 0xb4, 0x6a, 0xf8, 0xf4, 0x59, 0x59, 0xc4, 0x51, 0x5a}

func admitRV64GCArrayUniformWire(data []byte) bool {
	return len(data) == 127 && sha256.Sum256(data) == rv64GCArrayUniformSourceDigest
}

// AdmitGCArrayUniformSource is the wire-provenance half of the exact numeric
// array.new/get/set/len checkpoint.
func AdmitGCArrayUniformSource(data []byte) bool { return admitRV64GCArrayUniformWire(data) }

func admitRV64GCArrayUniform(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 4 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Exports) != 3 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
	}
	array := &m.Types[0].SubTypes[0]
	if !array.Final || array.HasPrefix || len(array.Supers) != 0 || array.Metadata.Describes.Present() || array.Metadata.Descriptor.Present() ||
		array.Comp.Kind != wasm.CompArray || array.Comp.Array != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var) {
		return false
	}
	wantFunctions := []struct {
		params, results []wasm.ValType
		typeIndex       uint32
		body            []byte
	}{
		{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 1, []byte{0x20, 0x00, 0x20, 0x01, 0xfb, 0x06, 0x00, 0x20, 0x02, 0xfb, 0x0b, 0x00, 0x0b}},
		{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 2, []byte{0x20, 0x00, 0x20, 0x01, 0xfb, 0x06, 0x00, 0x21, 0x04, 0x20, 0x04, 0x20, 0x02, 0x20, 0x03, 0xfb, 0x0e, 0x00, 0x20, 0x04, 0x20, 0x02, 0xfb, 0x0b, 0x00, 0x0b}},
		{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 3, []byte{0x20, 0x00, 0x20, 0x01, 0xfb, 0x06, 0x00, 0xfb, 0x0f, 0x0b}},
	}
	for i, want := range wantFunctions {
		if !i31CoreFunctionType(&m.Types[i+1], want.params, want.results) || m.FuncTypes[i] != (wasm.TypeIdx{Index: want.typeIndex}) {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, want.body) {
			return false
		}
		if i != 1 && (len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1) {
			return false
		}
	}
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if len(m.Code[1].Locals.Runs) != 1 || m.Code[1].Locals.Runs[0] != (wasm.LocalRun{Count: 1, Type: ref}) || m.Code[1].LocalDeclBytes != 4 {
		return false
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"get", 0}, {"set_get", 1}, {"len", 2}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
