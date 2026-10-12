//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingStructLinkProviderVariant = 1
	rv64GCTypeSubtypingStructLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingStructLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingStructLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f00038280808000010207858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingStructLinkWire("0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3301670002"),
}

var rv64GCTypeSubtypingStructLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingStructLinkWires))
	for i, wire := range rv64GCTypeSubtypingStructLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingStructLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingStructLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingStructLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingStructLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingStructLinkWire(data []byte) bool {
	return gcTypeSubtypingStructLinkSourceVariant(data) != 0
}

// GCTypeSubtypingStructLinkSourceVariant returns 1 for the exact .37
// provider, 2 for the exact .38 consumer, and zero for every other source.
func GCTypeSubtypingStructLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingStructLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingStructLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingStructLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
