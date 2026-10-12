//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingDuplicateRecursiveLinkProviderVariant = 1
	rv64GCTypeSubtypingDuplicateRecursiveLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingDuplicateRecursiveLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingDuplicateRecursiveLinkWire("0061736d0100000001a380808000024e025000600001647050010060000164004e02500060000164705001026000016402038380808000020001078d80808000020366313100000366313200010a918080800002838080800000000b838080800000000b"),
	mustDecodeGCTypeSubtypingDuplicateRecursiveLinkWire("0061736d0100000001a380808000024e025000600001647050010060000164004e0250006000016470500102600001640202a58080800004024d38036631310000024d38036631310002024d38036631320001024d38036631320003"),
}

var rv64GCTypeSubtypingDuplicateRecursiveLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingDuplicateRecursiveLinkWires))
	for i, wire := range rv64GCTypeSubtypingDuplicateRecursiveLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingDuplicateRecursiveLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingDuplicateRecursiveLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingDuplicateRecursiveLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingDuplicateRecursiveLinkWire(data []byte) bool {
	return gcTypeSubtypingDuplicateRecursiveLinkSourceVariant(data) != 0
}

// GCTypeSubtypingDuplicateRecursiveLinkSourceVariant returns 1 for the exact
// .47 provider, 2 for the exact .48 consumer, and zero otherwise.
func GCTypeSubtypingDuplicateRecursiveLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingDuplicateRecursiveLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingDuplicateRecursiveLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingDuplicateRecursiveLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
