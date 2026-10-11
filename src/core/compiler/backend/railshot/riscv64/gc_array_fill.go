//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayFillHex = "0061736d01000000019580808000055e78005e780160017f017f60000060037f7f7f000384808080000302030406958080800002640000410a410cfb06000b640101410cfb07010b07b080808000030d61727261795f6765745f6e746800000f61727261795f66696c6c2d6e756c6c00010a61727261795f66696c6c00020ab3808080000389808080000023012000fb0d010b8d8080800000d001410041004100fb10010b8d80808000002301200020012002fb10010b"

var rv64GCArrayFillSourceDigest = [sha256.Size]byte{0x08, 0x93, 0xca, 0xa7, 0xae, 0x7a, 0xb2, 0xd8, 0x70, 0x32, 0x9d, 0xa9, 0x69, 0x7d, 0x40, 0x5a, 0x51, 0x59, 0x2c, 0xb3, 0xec, 0xc1, 0xb4, 0xb8, 0x33, 0x78, 0x0e, 0xf9, 0xb2, 0x58, 0x01, 0x69}

func admitRV64GCArrayFillWire(data []byte) bool {
	if len(data) != 183 || sha256.Sum256(data) != rv64GCArrayFillSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 6, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayFill(m)
}

func AdmitGCArrayFillSource(data []byte) bool { return admitRV64GCArrayFillWire(data) }

func admitRV64GCArrayFill(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 3 || len(m.Code) != 3 || len(m.Globals) != 2 || len(m.Exports) != 3 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		!rv64ExactPackedArrayType(&m.Types[0], wasm.PackI8, false) || !rv64ExactPackedArrayType(&m.Types[1], wasm.PackI8, true) ||
		!i31CoreFunctionType(&m.Types[2], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}) || !i31CoreFunctionType(&m.Types[3], nil, nil) || !i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil) {
		return false
	}
	wantGlobals := []struct {
		typeIndex uint32
		mutable   bool
		body      []byte
	}{{0, false, []byte{0x41, 10, 0x41, 12, 0xfb, 6, 0, 0x0b}}, {1, true, []byte{0x41, 12, 0xfb, 7, 1, 0x0b}}}
	for i, want := range wantGlobals {
		g := &m.Globals[i]
		if g.Type.Mutable != want.mutable || g.Type.Type != wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: want.typeIndex}), false)) || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, want.body) {
			return false
		}
	}
	wantFuncTypes := []uint32{2, 3, 4}
	wantBodies := [][]byte{
		{0x23, 1, 0x20, 0, 0xfb, 0x0d, 1, 0x0b},
		{0xd0, 1, 0x41, 0, 0x41, 0, 0x41, 0, 0xfb, 0x10, 1, 0x0b},
		{0x23, 1, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 0x10, 1, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	wantExports := []string{"array_get_nth", "array_fill-null", "array_fill"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}
