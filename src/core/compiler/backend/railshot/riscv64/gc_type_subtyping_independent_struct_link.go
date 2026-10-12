//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingIndependentStructLinkProviderVariant = 1
	rv64GCTypeSubtypingIndependentStructLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingIndependentStructLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingIndependentStructLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f00038280808000010407858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingIndependentStructLinkWire("0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f0002888080800001024d3601670000"),
}

var rv64GCTypeSubtypingIndependentStructLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingIndependentStructLinkWires))
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingIndependentStructLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingIndependentStructLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingIndependentStructLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingIndependentStructLinkWire(data []byte) bool {
	return gcTypeSubtypingIndependentStructLinkSourceVariant(data) != 0
}

// GCTypeSubtypingIndependentStructLinkSourceVariant returns 1 for the exact
// .43 provider, 2 for the exact .44 consumer, and zero otherwise.
func GCTypeSubtypingIndependentStructLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingIndependentStructLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingIndependentStructLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingIndependentStructLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
