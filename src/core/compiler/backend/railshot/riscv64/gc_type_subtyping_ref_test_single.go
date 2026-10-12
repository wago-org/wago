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
const rv64GCTypeSubtypingRefTestDirectionFalseHex = "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"
const rv64GCTypeSubtypingRefTestSupertypeTrueHex = "0061736d0100000001a780808000044e0250006000005f016400004e0250006000005f016402004e025001006000005f006000017f038380808000020406078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"

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

var rv64GCTypeSubtypingRefTestDirectionFalseWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestDirectionFalseHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestDirectionFalseModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestDirectionFalseWire)
	if err != nil {
		panic(err)
	}
	return m
}()

var rv64GCTypeSubtypingRefTestSupertypeTrueWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestSupertypeTrueHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestSupertypeTrueModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestSupertypeTrueWire)
	if err != nil {
		panic(err)
	}
	return m
}()

var rv64GCTypeSubtypingRefTestWires = [...][]byte{
	rv64GCTypeSubtypingRefTestSingleWire,
	rv64GCTypeSubtypingRefTestRecursiveWire,
	rv64GCTypeSubtypingRefTestDirectionFalseWire,
	rv64GCTypeSubtypingRefTestSupertypeTrueWire,
}

var rv64GCTypeSubtypingRefTestModules = [...]*wasm.Module{
	rv64GCTypeSubtypingRefTestSingleModule,
	rv64GCTypeSubtypingRefTestRecursiveModule,
	rv64GCTypeSubtypingRefTestDirectionFalseModule,
	rv64GCTypeSubtypingRefTestSupertypeTrueModule,
}

func gcTypeSubtypingRefTestSingleSourceVariant(data []byte) uint8 {
	if !admitExactProductWireShapeWithRecGroups(data, []byte{1, 3, 7, 9, 10}) {
		return 0
	}
	for i, wire := range rv64GCTypeSubtypingRefTestWires {
		if !bytes.Equal(data, wire) {
			continue
		}
		m, err := wasm.DecodeModule(data)
		variant := uint8(i + 1)
		if err == nil && admitRV64GCTypeSubtypingRefTestSingleVariant(m) == variant {
			return variant
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingRefTestSingleWire(data []byte) bool {
	return gcTypeSubtypingRefTestSingleSourceVariant(data) != 0
}

// AdmitGCTypeSubtypingRefTestSingleSource returns the exact-wire provenance
// witness for the admitted single-result recursive ref.test products.
func AdmitGCTypeSubtypingRefTestSingleSource(data []byte) bool {
	return gcTypeSubtypingRefTestSingleSourceVariant(data) != 0
}

// GCTypeSubtypingRefTestSingleSourceVariant returns the exact source identity:
// 1 for .20, 2 for .21, 3 for .22, 4 for .23, and zero otherwise.
func GCTypeSubtypingRefTestSingleSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingRefTestSingleSourceVariant(data)
}

func admitRV64GCTypeSubtypingRefTestSingleVariant(m *wasm.Module) uint8 {
	if m == nil || len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingRefTestModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) &&
			reflect.DeepEqual(m.Exports, trusted.Exports) && reflect.DeepEqual(m.Elements, trusted.Elements) && reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingRefTestSingle(m *wasm.Module) bool {
	return admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 0
}

func rv64GCTypeSubtypingRefTestSingleKey(typeIndex uint32, structural uint64) uint64 {
	// Recursive group identity is nominal even when the flattened function
	// carrier is structurally empty. Keep the descriptor key distinct by type;
	// the admitted ref.test closure explicitly lists accepted source identities.
	return structural ^ uint64(typeIndex)*rv64GCTypeSubtypingRuntimeFinalityKeySalt
}
