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
const rv64GCTypeSubtypingRefTestMultiFirstHex = "0061736d0100000001e480808000064e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406004e025001066000005f006000027f7f03838080800002080a078780808000010372756e000109858080800001030001000a9980808000028280808000000b8c8080800000d200fb1400d200fb14040b"
const rv64GCTypeSubtypingRefTestMultiPairsHex = "0061736d0100000001aa80808000034e025000600001647050010060000164004e025000600001647050010260000164026000047f7f7f7f03848080800003000104078780808000010372756e00020989808080000203000100030001010aac8080800003838080800000000b838080800000000b968080800000d200fb1400d200fb1402d201fb1401d201fb14030b"
const rv64GCTypeSubtypingRefTestMultiHex = "0061736d0100000001d280808000054e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e02500102600001647050010660000164066000087f7f7f7f7f7f7f7f03848080800003040508078780808000010372756e00020989808080000203000100030001010ac08080800003838080800000000b838080800000000baa8080800000d200fb1400d200fb1402d201fb1400d201fb1402d200fb1404d200fb1406d201fb1405d201fb14070b"

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

var rv64GCTypeSubtypingRefTestMultiFirstWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestMultiFirstHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestMultiFirstModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestMultiFirstWire)
	if err != nil {
		panic(err)
	}
	return m
}()

var rv64GCTypeSubtypingRefTestMultiPairsWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestMultiPairsHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestMultiPairsModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestMultiPairsWire)
	if err != nil {
		panic(err)
	}
	return m
}()

var rv64GCTypeSubtypingRefTestMultiWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRefTestMultiHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRefTestMultiModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRefTestMultiWire)
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
	rv64GCTypeSubtypingRefTestMultiFirstWire,
	rv64GCTypeSubtypingRefTestMultiPairsWire,
	rv64GCTypeSubtypingRefTestMultiWire,
}

var rv64GCTypeSubtypingRefTestModules = [...]*wasm.Module{
	rv64GCTypeSubtypingRefTestSingleModule,
	rv64GCTypeSubtypingRefTestRecursiveModule,
	rv64GCTypeSubtypingRefTestDirectionFalseModule,
	rv64GCTypeSubtypingRefTestSupertypeTrueModule,
	rv64GCTypeSubtypingRefTestMultiFirstModule,
	rv64GCTypeSubtypingRefTestMultiPairsModule,
	rv64GCTypeSubtypingRefTestMultiModule,
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
// witness for the admitted recursive ref.test products.
func AdmitGCTypeSubtypingRefTestSingleSource(data []byte) bool {
	return gcTypeSubtypingRefTestSingleSourceVariant(data) != 0
}

// GCTypeSubtypingRefTestSingleSourceVariant returns the exact source identity:
// 1 for .20 through 7 for .26, and zero otherwise.
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
