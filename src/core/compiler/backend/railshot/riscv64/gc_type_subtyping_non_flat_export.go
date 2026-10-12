//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingNonFlatExportWire = mustDecodeGCTypeSubtypingNonFlatExportWire("0061736d0100000001a8808080000450006000017d5001006000017d4f01016000017d4e036000017d50006000017d5001046000017d03878080800006000102030405079f80808000060266310000026632000102663300020266340003026635000402663600050ac9808080000687808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b")

var rv64GCTypeSubtypingNonFlatExportModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(append([]byte(nil), rv64GCTypeSubtypingNonFlatExportWire...))
	if err != nil {
		panic(err)
	}
	return m
}()

func mustDecodeGCTypeSubtypingNonFlatExportWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func admitRV64GCTypeSubtypingNonFlatExportWire(data []byte) bool {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 7, 10}) || !bytes.Equal(data, rv64GCTypeSubtypingNonFlatExportWire) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingNonFlatExport(m)
}

func GCTypeSubtypingNonFlatExportSource(data []byte) bool {
	return admitRV64GCTypeSubtypingNonFlatExportWire(data)
}

func admitRV64GCTypeSubtypingNonFlatExport(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		len(m.Globals) != 0 || m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	trusted := rv64GCTypeSubtypingNonFlatExportModule
	return reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
		reflect.DeepEqual(m.Code, trusted.Code) && reflect.DeepEqual(m.Exports, trusted.Exports)
}
