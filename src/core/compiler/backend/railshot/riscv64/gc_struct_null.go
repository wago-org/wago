//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCStructNullHex = "0061736d01000000018a80808000025f027f007f0160000003838080800002010107a580808000020f7374727563742e6765742d6e756c6c00000f7374727563742e7365742d6e756c6c00010aa480808000028c80808000010163002000fb0200011a0b8d808080000101630020004100fb0500010b"

var rv64GCStructNullSourceDigest = [sha256.Size]byte{0xb0, 0x19, 0x11, 0x99, 0x0b, 0xd0, 0x67, 0x8f, 0x48, 0x4a, 0xfa, 0xe1, 0xa2, 0x8d, 0xec, 0x4b, 0x44, 0xc3, 0x73, 0x08, 0xdb, 0x5f, 0x5f, 0xdc, 0xe7, 0xaa, 0x30, 0xb0, 0x02, 0x75, 0xfd, 0x31}

func admitRV64GCStructNullWire(data []byte) bool {
	if len(data) != 118 || sha256.Sum256(data) != rv64GCStructNullSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCStructNull(m)
}

func AdmitGCStructNullSource(data []byte) bool { return admitRV64GCStructNullWire(data) }

func admitRV64GCStructNull(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		!i31CoreFunctionType(&m.Types[1], nil, nil) {
		return false
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	structure := &m.Types[0].SubTypes[0]
	if !structure.Final || structure.HasPrefix || len(structure.Supers) != 0 || structure.Metadata.Describes.Present() || structure.Metadata.Descriptor.Present() || structure.Comp.Kind != wasm.CompStruct ||
		len(structure.Comp.Fields) != 2 || structure.Comp.Fields[0] != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const) || structure.Comp.Fields[1] != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var) {
		return false
	}
	wantBodies := [][]byte{{0x20, 0, 0xfb, 2, 0, 1, 0x1a, 0x0b}, {0x20, 0, 0x41, 0, 0xfb, 5, 0, 1, 0x0b}}
	for i, fn := range m.Code {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: 1}) || len(fn.Locals.Runs) != 1 || fn.LocalDeclBytes != 4 || fn.Locals.Runs[0].Count != 1 || fn.Locals.Runs[0].Type != wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)) ||
			len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"struct.get-null", "struct.set-null"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}
