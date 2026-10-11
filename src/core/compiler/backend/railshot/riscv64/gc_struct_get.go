//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCStructGetSourceDigest = [sha256.Size]byte{0xf5, 0xfc, 0x57, 0xa9, 0xa6, 0xb9, 0x59, 0xa1, 0xa6, 0x89, 0x38, 0x5c, 0xb7, 0x90, 0x50, 0xb6, 0x99, 0x8c, 0x86, 0x7c, 0x61, 0xea, 0xfd, 0x65, 0xff, 0x03, 0xb2, 0xd5, 0x7d, 0x12, 0x8f, 0xcf}

func admitRV64GCStructGetWire(data []byte) bool {
	return len(data) == 65 && sha256.Sum256(data) == rv64GCStructGetSourceDigest
}

// AdmitGCStructGetSource is the wire-provenance half of the exact allocating
// struct.new_default/struct.get checkpoint.
func AdmitGCStructGetSource(data []byte) bool { return admitRV64GCStructGetWire(data) }

func admitRV64GCStructGet(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 2 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Exports) != 1 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	if len(m.Types[0].SubTypes) != 1 || len(m.Types[1].SubTypes) != 1 {
		return false
	}
	structure, function := &m.Types[0].SubTypes[0], &m.Types[1].SubTypes[0]
	if !structure.Final || structure.HasPrefix || len(structure.Supers) != 0 || structure.Metadata.Describes.Present() || structure.Metadata.Descriptor.Present() ||
		structure.Comp.Kind != wasm.CompStruct || len(structure.Comp.Fields) != 1 || structure.Comp.Fields[0] != wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var) ||
		!i31CoreFunctionType(&m.Types[1], nil, []wasm.ValType{wasm.I32}) || m.FuncTypes[0] != (wasm.TypeIdx{Index: 1}) {
		return false
	}
	fn := &m.Code[0]
	return len(fn.Locals.Runs) == 0 && fn.LocalDeclBytes == 1 && len(fn.Body.Instrs) == 0 && len(fn.Body.BodyBytes) == 0 &&
		bytes.Equal(fn.BodyBytes, []byte{0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}) &&
		m.Exports[0].Name == "get" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 && function.Final
}
