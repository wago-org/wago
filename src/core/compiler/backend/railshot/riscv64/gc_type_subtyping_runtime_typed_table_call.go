//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRuntimeTypedTableCallHex = "0061736d01000000019580808000045000600000500100600000500101600000600000038680808000050102030303048680808000016301010202079780808000030372756e0002056661696c310003056661696c320004098f8080800001060041000b630102d2000bd2010b0ac780808000058280808000000b8280808000000b9b8080800000410011000041011100004100110100410111010041011102000b87808080000041001102000b87808080000041001103000b"

var rv64GCTypeSubtypingRuntimeTypedTableCallWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRuntimeTypedTableCallHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRuntimeTypedTableCallModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRuntimeTypedTableCallWire)
	if err != nil {
		panic(err)
	}
	return m
}()

func admitRV64GCTypeSubtypingRuntimeTypedTableCallWire(data []byte) bool {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 4, 7, 9, 10}) || !bytes.Equal(data, rv64GCTypeSubtypingRuntimeTypedTableCallWire) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingRuntimeTypedTableCall(m)
}

// AdmitGCTypeSubtypingRuntimeTypedTableCallSource returns the exact-wire
// provenance witness for official gc/type-subtyping.19.wasm.
func AdmitGCTypeSubtypingRuntimeTypedTableCallSource(data []byte) bool {
	return admitRV64GCTypeSubtypingRuntimeTypedTableCallWire(data)
}

func admitRV64GCTypeSubtypingRuntimeTypedTableCall(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	trusted := rv64GCTypeSubtypingRuntimeTypedTableCallModule
	return reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
		reflect.DeepEqual(m.Tables, trusted.Tables) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
		reflect.DeepEqual(m.Elements, trusted.Elements) && reflect.DeepEqual(m.Code, trusted.Code)
}

func rv64GCTypeSubtypingRuntimeTypedTableKey(typeIndex uint32, structural uint64) uint64 {
	// The four declared function types have identical structural carriers but
	// distinct nominal identities. Stable private salts preserve that identity
	// in descriptors without changing the shared structural key store.
	return structural ^ uint64(typeIndex)*rv64GCTypeSubtypingRuntimeFinalityKeySalt
}
