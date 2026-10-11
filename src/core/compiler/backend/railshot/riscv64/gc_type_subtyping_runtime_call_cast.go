//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRuntimeCallCastHex = "0061736d01000000019a808080000450006000017050010060000163015001016000016302600000038b808080000a00010203030303030303048580808000017001030307b780808000070372756e0003056661696c310004056661696c320005056661696c330006056661696c340007056661696c350008056661696c360009098f80808000010441000b03d2000bd2010bd2020b0a80828080000a848080800000d0700b848080800000d0010b848080800000d0020bf98080800000027041001100000b027041011100000b027041021100000b02630141011101000b02630141021101000b02630241021102000b02630041002500fb16000b02630041012500fb16000b02630041022500fb16000b02630141012500fb16010b02630141022500fb16010b02630241022500fb16020b0c000b8d808080000002630141001101000b0c000b8d808080000002630141001102000b0c000b8d808080000002630141011102000b0c000b8b808080000041002500fb16010c000b8b808080000041002500fb16020c000b8b808080000041012500fb16020c000b"

var rv64GCTypeSubtypingRuntimeCallCastWire = mustDecodeGCTypeSubtypingRuntimeCallCastWire(rv64GCTypeSubtypingRuntimeCallCastHex)

var rv64GCTypeSubtypingRuntimeCallCastModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRuntimeCallCastWire)
	if err != nil {
		panic(err)
	}
	return m
}()

func mustDecodeGCTypeSubtypingRuntimeCallCastWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func admitRV64GCTypeSubtypingRuntimeCallCastWire(data []byte) bool {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 4, 7, 9, 10}) || !bytes.Equal(data, rv64GCTypeSubtypingRuntimeCallCastWire) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingRuntimeCallCast(m)
}

// AdmitGCTypeSubtypingRuntimeCallCastSource returns the exact-wire provenance
// witness for the official gc/type-subtyping runtime call/cast module.
func AdmitGCTypeSubtypingRuntimeCallCastSource(data []byte) bool {
	return admitRV64GCTypeSubtypingRuntimeCallCastWire(data)
}

func admitRV64GCTypeSubtypingRuntimeCallCast(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	trusted := rv64GCTypeSubtypingRuntimeCallCastModule
	return reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
		reflect.DeepEqual(m.Tables, trusted.Tables) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
		reflect.DeepEqual(m.Elements, trusted.Elements) && reflect.DeepEqual(m.Code, trusted.Code)
}
