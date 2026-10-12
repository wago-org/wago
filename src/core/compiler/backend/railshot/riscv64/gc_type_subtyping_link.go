//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingLinkProviderVariant = 1
	rv64GCTypeSubtypingLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630203848080800003000102079080808000030266300000026631000102663200020a9c8080800003848080800000d0700b848080800000d0010b848080800000d0020b"),
	mustDecodeGCTypeSubtypingLinkWire("0061736d01000000019780808000035000600001705001006000016301500101600001630202ab8080800006014d0266300000014d0266310000014d0266310001014d0266320000014d0266320001014d0266320002"),
}

var rv64GCTypeSubtypingLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingLinkWires))
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingLinkWire(data []byte) bool {
	return gcTypeSubtypingLinkSourceVariant(data) != 0
}

// GCTypeSubtypingLinkSourceVariant returns 1 for the exact .29 provider, 2 for
// the exact .30 consumer, and zero for every other source.
func GCTypeSubtypingLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
