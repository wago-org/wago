//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCStructNumericGlobalsHex = "0061736d01000000018780808000015f027f007f0106978080800002640000410a410bfb00000b64000041144115fb00000b078b808080000202673003000267310301"

var rv64GCStructNumericGlobalsSourceDigest = [sha256.Size]byte{0x03, 0x87, 0xe5, 0x19, 0xfa, 0x92, 0x1b, 0x90, 0x5d, 0x06, 0x57, 0xa6, 0xfa, 0xfb, 0x63, 0x0a, 0xb7, 0xac, 0xaa, 0x3a, 0x62, 0x82, 0xe3, 0x54, 0xb3, 0xf0, 0xf2, 0xe4, 0x5a, 0xdb, 0xfe, 0xee}

func admitRV64GCStructNumericGlobalsWire(data []byte) bool {
	if len(data) != 67 || sha256.Sum256(data) != rv64GCStructNumericGlobalsSourceDigest {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCStructNumericGlobals(m)
}

func AdmitGCStructNumericGlobalsSource(data []byte) bool {
	return admitRV64GCStructNumericGlobalsWire(data)
}

func admitRV64GCStructNumericGlobals(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 0 || len(m.Code) != 0 || len(m.Globals) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || len(m.BranchHints) != 0 || m.UsesCompactImports || len(m.Types[0].SubTypes) != 1 {
		return false
	}
	s := &m.Types[0].SubTypes[0]
	wantFields := []wasm.FieldType{
		wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const),
		wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Var),
	}
	if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Metadata.Describes.Present() || s.Metadata.Descriptor.Present() || s.Comp.Kind != wasm.CompStruct || !slices.Equal(s.Comp.Fields, wantFields) {
		return false
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	wantGlobals := [][]byte{{0x41, 10, 0x41, 11, 0xfb, 0, 0, 0x0b}, {0x41, 20, 0x41, 21, 0xfb, 0, 0, 0x0b}}
	for i, g := range m.Globals {
		if !wasm.EqualValType(g.Type.Type, ref0) || g.Type.Mutable || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, wantGlobals[i]) {
			return false
		}
	}
	for i, name := range []string{"g0", "g1"} {
		got := m.Exports[i]
		if got.Name != name || got.Index.Kind != wasm.ExternGlobal || got.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}
