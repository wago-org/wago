//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingExtendedProjectionLinkProviderVariant = 1
	rv64GCTypeSubtypingExtendedProjectionLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingExtendedProjectionLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingExtendedProjectionLinkWire("0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001026000005001035f056400006402006400006402006404004e025001046000005f00038280808000010607858080800001016800000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingExtendedProjectionLinkWire("0061736d0100000001c380808000044e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001046000005f00028f8080800002024d3701680000024d3701680004"),
}

var rv64GCTypeSubtypingExtendedProjectionLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingExtendedProjectionLinkWires))
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingExtendedProjectionLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingExtendedProjectionLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		if bytes.Equal(data, wire) {
			if !admitExactProductWireShapeWithRecGroups(data, [][]byte{{1, 3, 7, 10}, {1, 2}}[i]) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingExtendedProjectionLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingExtendedProjectionLinkWire(data []byte) bool {
	return gcTypeSubtypingExtendedProjectionLinkSourceVariant(data) != 0
}

// GCTypeSubtypingExtendedProjectionLinkSourceVariant returns 1 for the exact
// .45 provider, 2 for the exact .46 consumer, and zero otherwise.
func GCTypeSubtypingExtendedProjectionLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingExtendedProjectionLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingExtendedProjectionLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingExtendedProjectionLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
