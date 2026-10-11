//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCStructMutationHex = "0061736d01000000018e80808000035f017f016000017f60017f017f038380808000020102078d80808000020367657400000373657400010aac8080800002898080800000fb0100fb0200000b988080800001016300fb0100210120012000fb0500002001fb0200000b"

var rv64GCStructMutationSourceDigest = [sha256.Size]byte{0xe9, 0xf7, 0xa7, 0xec, 0x88, 0xc5, 0x66, 0x84, 0xad, 0x5b, 0x96, 0xe2, 0xa5, 0x47, 0x17, 0x65, 0xab, 0x28, 0x35, 0xdd, 0xea, 0x14, 0x06, 0x90, 0x06, 0xda, 0x51, 0xa9, 0x6e, 0xd5, 0xe8, 0x91}

func admitRV64GCStructMutationWire(data []byte) bool {
	if len(data) != 106 || sha256.Sum256(data) != rv64GCStructMutationSourceDigest {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCStructMutation(m)
}

// AdmitGCStructMutationSource is the wire-provenance half of the bounded
// struct.new_default/struct.get/struct.set checkpoint.
func AdmitGCStructMutationSource(data []byte) bool { return admitRV64GCStructMutationWire(data) }

func admitRV64GCStructMutation(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 3 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for i := range m.Types {
		if len(m.Types[i].SubTypes) != 1 {
			return false
		}
	}
	structure := &m.Types[0].SubTypes[0]
	if !structure.Final || structure.HasPrefix || len(structure.Supers) != 0 || structure.Metadata.Describes.Present() || structure.Metadata.Descriptor.Present() ||
		structure.Comp.Kind != wasm.CompStruct || len(structure.Comp.Fields) != 1 || structure.Comp.Fields[0] != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var) ||
		!i31CoreFunctionType(&m.Types[1], nil, []wasm.ValType{wasm.I32}) ||
		!i31CoreFunctionType(&m.Types[2], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) ||
		m.FuncTypes[0] != (wasm.TypeIdx{Index: 1}) || m.FuncTypes[1] != (wasm.TypeIdx{Index: 2}) {
		return false
	}
	get, set := &m.Code[0], &m.Code[1]
	if len(get.Locals.Runs) != 0 || get.LocalDeclBytes != 1 || len(get.Body.Instrs) != 0 || len(get.Body.BodyBytes) != 0 ||
		!bytes.Equal(get.BodyBytes, []byte{0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}) {
		return false
	}
	if len(set.Locals.Runs) != 1 || set.LocalDeclBytes != 4 || set.Locals.Runs[0].Count != 1 || set.Locals.Runs[0].Type != wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)) || len(set.Body.Instrs) != 0 || len(set.Body.BodyBytes) != 0 ||
		!bytes.Equal(set.BodyBytes, []byte{0xfb, 0x01, 0x00, 0x21, 0x01, 0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}) {
		return false
	}
	return m.Exports[0].Name == "get" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 &&
		m.Exports[1].Name == "set" && m.Exports[1].Index.Kind == wasm.ExternFunc && m.Exports[1].Index.Index == 1
}
