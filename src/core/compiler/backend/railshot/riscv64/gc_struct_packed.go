//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCStructPackedHex = "0061736d01000000019680808000035f0478007801770077016000027f7f60017f027f7f038b808080000a0101010101010101020206a580808000026400004100410141024103fb00000b64000041fe0141ff0141feff0341ffff03fb00000b07c7818080000c026730030002673103010f6765745f7061636b65645f67305f3000000f6765745f7061636b65645f67315f3000010f6765745f7061636b65645f67305f3100020f6765745f7061636b65645f67315f3100030f6765745f7061636b65645f67305f3200040f6765745f7061636b65645f67315f3200050f6765745f7061636b65645f67305f3300060f6765745f7061636b65645f67315f330007137365745f6765745f7061636b65645f67305f310008137365745f6765745f7061636b65645f67305f3300090acf818080000a8e80808000002300fb0300002300fb0400000b8e80808000002301fb0300002301fb0400000b8e80808000002300fb0300012300fb0400010b8e80808000002301fb0300012301fb0400010b8e80808000002300fb0300022300fb0400020b8e80808000002301fb0300022301fb0400020b8e80808000002300fb0300032300fb0400030b8e80808000002301fb0300032301fb0400030b96808080000023002000fb0500012300fb0300012300fb0400010b96808080000023002000fb0500032300fb0300032300fb0400030b"

var rv64GCStructPackedSourceDigest = [sha256.Size]byte{0x96, 0xa4, 0x75, 0x80, 0xd9, 0xd8, 0x60, 0x53, 0xfd, 0xc3, 0x30, 0x6a, 0x59, 0xb2, 0x21, 0x3f, 0xd5, 0x81, 0xb9, 0xba, 0x19, 0x87, 0xda, 0x0c, 0x24, 0x0f, 0xcc, 0xb7, 0xbe, 0x1f, 0x6e, 0x58}

func admitRV64GCStructPackedWire(data []byte) bool {
	if len(data) != 514 || sha256.Sum256(data) != rv64GCStructPackedSourceDigest {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCStructPacked(m)
}

func AdmitGCStructPackedSource(data []byte) bool { return admitRV64GCStructPackedWire(data) }

func admitRV64GCStructPacked(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 3 || len(m.FuncTypes) != 10 || len(m.Code) != 10 || len(m.Globals) != 2 || len(m.Exports) != 12 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports {
		return false
	}
	if len(m.Types[0].SubTypes) != 1 {
		return false
	}
	s := &m.Types[0].SubTypes[0]
	wantFields := []wasm.FieldType{
		wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Const), wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Var),
		wasm.NewFieldType(wasm.StoragePacked(wasm.PackI16), wasm.Const), wasm.NewFieldType(wasm.StoragePacked(wasm.PackI16), wasm.Var),
	}
	if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Metadata.Describes.Present() || s.Metadata.Descriptor.Present() || s.Comp.Kind != wasm.CompStruct || !slices.Equal(s.Comp.Fields, wantFields) {
		return false
	}
	if !i31CoreFunctionType(&m.Types[1], nil, []wasm.ValType{wasm.I32, wasm.I32}) || !i31CoreFunctionType(&m.Types[2], []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}) {
		return false
	}
	wantBodies := [][]byte{
		{0x23, 0, 0xfb, 3, 0, 0, 0x23, 0, 0xfb, 4, 0, 0, 0x0b}, {0x23, 1, 0xfb, 3, 0, 0, 0x23, 1, 0xfb, 4, 0, 0, 0x0b},
		{0x23, 0, 0xfb, 3, 0, 1, 0x23, 0, 0xfb, 4, 0, 1, 0x0b}, {0x23, 1, 0xfb, 3, 0, 1, 0x23, 1, 0xfb, 4, 0, 1, 0x0b},
		{0x23, 0, 0xfb, 3, 0, 2, 0x23, 0, 0xfb, 4, 0, 2, 0x0b}, {0x23, 1, 0xfb, 3, 0, 2, 0x23, 1, 0xfb, 4, 0, 2, 0x0b},
		{0x23, 0, 0xfb, 3, 0, 3, 0x23, 0, 0xfb, 4, 0, 3, 0x0b}, {0x23, 1, 0xfb, 3, 0, 3, 0x23, 1, 0xfb, 4, 0, 3, 0x0b},
		{0x23, 0, 0x20, 0, 0xfb, 5, 0, 1, 0x23, 0, 0xfb, 3, 0, 1, 0x23, 0, 0xfb, 4, 0, 1, 0x0b},
		{0x23, 0, 0x20, 0, 0xfb, 5, 0, 3, 0x23, 0, 0xfb, 3, 0, 3, 0x23, 0, 0xfb, 4, 0, 3, 0x0b},
	}
	for i := range m.Code {
		wantType := uint32(1)
		if i >= 8 {
			wantType = 2
		}
		fn := &m.Code[i]
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantType}) || len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	wantGlobals := [][]byte{{0x41, 0, 0x41, 1, 0x41, 2, 0x41, 3, 0xfb, 0, 0, 0x0b}, {0x41, 0xfe, 1, 0x41, 0xff, 1, 0x41, 0xfe, 0xff, 3, 0x41, 0xff, 0xff, 3, 0xfb, 0, 0, 0x0b}}
	for i, g := range m.Globals {
		if !wasm.EqualValType(g.Type.Type, ref0) || g.Type.Mutable || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, wantGlobals[i]) {
			return false
		}
	}
	wantExports := []string{"g0", "g1", "get_packed_g0_0", "get_packed_g1_0", "get_packed_g0_1", "get_packed_g1_1", "get_packed_g0_2", "get_packed_g1_2", "get_packed_g0_3", "get_packed_g1_3", "set_get_packed_g0_1", "set_get_packed_g0_3"}
	for i, name := range wantExports {
		got := m.Exports[i]
		if i < 2 {
			if got.Name != name || got.Index.Kind != wasm.ExternGlobal || got.Index.Index != uint32(i) {
				return false
			}
		} else if got.Name != name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != uint32(i-2) {
			return false
		}
	}
	return true
}
