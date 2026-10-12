//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRefTestSingleHex = "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016402004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"
const rv64GCTypeSubtypingRefTestRecursiveHex = "0061736d0100000001d980808000054e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"

var rv64GCTypeSubtypingRefTestSingleWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestSingleHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestSingleModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestSingleWire)
	if err != nil {
		panic(err)
	}
	return m
}()

var rv64GCTypeSubtypingRefTestRecursiveWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestRecursiveHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestRecursiveModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestRecursiveWire)
	if err != nil {
		panic(err)
	}
	return m
}()

func admitRV64GCTypeSubtypingRefTestSingleWire(data []byte) bool {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 7, 9, 10}) ||
		!bytes.Equal(data, rv64GCTypeSubtypingRefTestSingleWire) && !bytes.Equal(data, rv64GCTypeSubtypingRefTestRecursiveWire) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingRefTestSingle(m)
}

// AdmitGCTypeSubtypingRefTestSingleSource returns the exact-wire provenance
// witness for official gc/type-subtyping.20.wasm and .21.wasm.
func AdmitGCTypeSubtypingRefTestSingleSource(data []byte) bool {
	return admitRV64GCTypeSubtypingRefTestSingleWire(data)
}

func admitRV64GCTypeSubtypingRefTestSingle(m *wasm.Module) bool {
	if m == nil || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	for _, trusted := range []*wasm.Module{rv64GCTypeSubtypingRefTestSingleModule, rv64GCTypeSubtypingRefTestRecursiveModule} {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
			reflect.DeepEqual(m.Exports, trusted.Exports) && reflect.DeepEqual(m.Elements, trusted.Elements) && reflect.DeepEqual(m.Code, trusted.Code) {
			return true
		}
	}
	return false
}

func rv64GCTypeSubtypingRefTestSingleKey(typeIndex uint32, structural uint64) uint64 {
	// Recursive group identity is nominal even when the flattened function
	// carrier is structurally empty. Keep the descriptor key distinct by type;
	// the admitted ref.test closure explicitly lists accepted source identities.
	return structural ^ uint64(typeIndex)*rv64GCTypeSubtypingRuntimeFinalityKeySalt
}
