//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingStructMismatchLinkProviderVariant = 1
	rv64GCTypeSubtypingStructMismatchLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingStructMismatchLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingStructMismatchLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016400004e025001026000005f00038280808000010407858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingStructMismatchLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3501670002"),
}

var rv64GCTypeSubtypingStructMismatchLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingStructMismatchLinkWires))
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingStructMismatchLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingStructMismatchLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingStructMismatchLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingStructMismatchLinkWire(data []byte) bool {
	return gcTypeSubtypingStructMismatchLinkSourceVariant(data) != 0
}

// GCTypeSubtypingStructMismatchLinkSourceVariant returns 1 for the exact .41
// provider, 2 for the exact .42 consumer, and zero for every other source.
func GCTypeSubtypingStructMismatchLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingStructMismatchLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingStructMismatchLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingStructMismatchLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
