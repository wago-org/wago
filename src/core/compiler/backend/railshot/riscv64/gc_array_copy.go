//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayCopyHex = "0061736d01000000019580808000055e78005e780160017f017f60000060037f7f7f000387808080000602030304030306958080800002640000410a410cfb06000b640101410cfb07010b078581808000060d61727261795f6765745f6e746800001461727261795f636f70792d6e756c6c2d6c65667400011561727261795f636f70792d6e756c6c2d726967687400020a61727261795f636f707900031961727261795f636f70795f6f7665726c61705f746573742d3100041961727261795f636f70795f6f7665726c61705f746573742d3200050c8180808000010a9a818080000689808080000023012000fb0d010b908080800000d0014100230041004100fb1101000b90808080000023014100d00041004100fb1101000b90808080000023012000230020012002fb1101000ba180808000010164014100410cfb09010021002000410120004100410bfb110101200024010ba180808000010164014100410cfb09010021002000410020004101410bfb110101200024010b0b8f8080800001010c6162636465666768696a6b6c"

var rv64GCArrayCopySourceDigest = [sha256.Size]byte{0x3c, 0xe0, 0xc2, 0x21, 0x05, 0x57, 0x16, 0x18, 0x83, 0x2b, 0x6d, 0x97, 0x16, 0x4a, 0x26, 0xe4, 0xb7, 0xde, 0xe0, 0x35, 0xf5, 0x40, 0x95, 0x74, 0x22, 0xb8, 0x87, 0xc4, 0xc0, 0x4d, 0x4f, 0x35}

func admitRV64GCArrayCopyWire(data []byte) bool {
	if len(data) != 402 || sha256.Sum256(data) != rv64GCArrayCopySourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 6, 7, 12, 10, 11}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayCopy(m)
}

func AdmitGCArrayCopySource(data []byte) bool { return admitRV64GCArrayCopyWire(data) }

func admitRV64GCArrayCopy(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 6 || len(m.Code) != 6 || len(m.Globals) != 2 || len(m.Exports) != 6 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 1 || m.DataCount == nil || *m.DataCount != 1 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports ||
		m.Data[0].Mode.Kind != wasm.DataPassive || !bytes.Equal(m.Data[0].Init, []byte("abcdefghijkl")) ||
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
	wantFuncTypes := []uint32{2, 3, 3, 4, 3, 3}
	wantLocals := []int{0, 0, 0, 0, 1, 1}
	wantBodies := [][]byte{
		{0x23, 1, 0x20, 0, 0xfb, 0x0d, 1, 0x0b},
		{0xd0, 1, 0x41, 0, 0x23, 0, 0x41, 0, 0x41, 0, 0xfb, 0x11, 1, 0, 0x0b},
		{0x23, 1, 0x41, 0, 0xd0, 0, 0x41, 0, 0x41, 0, 0xfb, 0x11, 1, 0, 0x0b},
		{0x23, 1, 0x20, 0, 0x23, 0, 0x20, 1, 0x20, 2, 0xfb, 0x11, 1, 0, 0x0b},
		{0x41, 0, 0x41, 12, 0xfb, 9, 1, 0, 0x21, 0, 0x20, 0, 0x41, 1, 0x20, 0, 0x41, 0, 0x41, 11, 0xfb, 0x11, 1, 1, 0x20, 0, 0x24, 1, 0x0b},
		{0x41, 0, 0x41, 12, 0xfb, 9, 1, 0, 0x21, 0, 0x20, 0, 0x41, 0, 0x20, 0, 0x41, 1, 0x41, 11, 0xfb, 0x11, 1, 1, 0x20, 0, 0x24, 1, 0x0b},
	}
	for i := range m.Code {
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) || len(fn.Locals.Runs) != wantLocals[i] || fn.LocalDeclBytes != map[bool]uint32{false: 1, true: 4}[wantLocals[i] != 0] || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
		if wantLocals[i] == 1 && (fn.Locals.Runs[0].Count != 1 || fn.Locals.Runs[0].Type != wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))) {
			return false
		}
	}
	wantExports := []string{"array_get_nth", "array_copy-null-left", "array_copy-null-right", "array_copy", "array_copy_overlap_test-1", "array_copy_overlap_test-2"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}
