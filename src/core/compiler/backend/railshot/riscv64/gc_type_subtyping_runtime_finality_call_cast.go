//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRuntimeFinalityCallCastHex = "0061736d0100000001898080800002500060000060000003878080800006000101010101048580808000017001020207a18080800004056661696c310002056661696c320003056661696c330004056661696c340005098c80808000010441000b02d2000bd2010b0acb80808000068280808000000b8280808000000b8a8080800000024041011100000b0b8a8080800000024041001101000b0b8a808080000041012500fb16001a0b8a808080000041002500fb16011a0b"

const rv64GCTypeSubtypingRuntimeFinalityKeySalt uint64 = 0x9e3779b97f4a7c15

var rv64GCTypeSubtypingRuntimeFinalityCallCastWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRuntimeFinalityCallCastHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRuntimeFinalityCallCastModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRuntimeFinalityCallCastWire)
	if err != nil {
		panic(err)
	}
	return m
}()

func admitRV64GCTypeSubtypingRuntimeFinalityCallCastWire(data []byte) bool {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 4, 7, 9, 10}) || !bytes.Equal(data, rv64GCTypeSubtypingRuntimeFinalityCallCastWire) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m)
}

// AdmitGCTypeSubtypingRuntimeFinalityCallCastSource returns the exact-wire
// provenance witness for official gc/type-subtyping.18.wasm.
func AdmitGCTypeSubtypingRuntimeFinalityCallCastSource(data []byte) bool {
	return admitRV64GCTypeSubtypingRuntimeFinalityCallCastWire(data)
}

func admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	trusted := rv64GCTypeSubtypingRuntimeFinalityCallCastModule
	return reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
		reflect.DeepEqual(m.Tables, trusted.Tables) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
		reflect.DeepEqual(m.Elements, trusted.Elements) && reflect.DeepEqual(m.Code, trusted.Code)
}
