//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingStructProjectionLinkProviderVariant = 1
	rv64GCTypeSubtypingStructProjectionLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingStructProjectionLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingStructProjectionLinkWire("0061736d0100000001b980808000034e02500060000050005f016400004e02500060000050005f016402004e025001026000005001035f05640000640200640000640200640400038280808000010407858080800001016700000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingStructProjectionLinkWire("0061736d0100000001b980808000034e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f0564000064000064020064020064040002888080800001024d3401670004"),
}

var rv64GCTypeSubtypingStructProjectionLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingStructProjectionLinkWires))
	for i, wire := range rv64GCTypeSubtypingStructProjectionLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingStructProjectionLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingStructProjectionLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingStructProjectionLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingStructProjectionLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingStructProjectionLinkWire(data []byte) bool {
	return gcTypeSubtypingStructProjectionLinkSourceVariant(data) != 0
}

// GCTypeSubtypingStructProjectionLinkSourceVariant returns 1 for the exact
// .39 provider, 2 for the exact .40 consumer, and zero for every other source.
func GCTypeSubtypingStructProjectionLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingStructProjectionLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingStructProjectionLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingStructProjectionLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
