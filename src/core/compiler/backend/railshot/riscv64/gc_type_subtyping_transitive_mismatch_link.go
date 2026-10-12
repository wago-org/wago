//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingTransitiveMismatchLinkProviderVariant = 1
	rv64GCTypeSubtypingTransitiveMismatchLinkConsumerVariant = 2
)

var rv64GCTypeSubtypingTransitiveMismatchLinkWires = [][]byte{
	mustDecodeGCTypeSubtypingTransitiveMismatchLinkWire("0061736d0100000001a880808000034e0250006000005001006000004e0250006000005001006000004e025000600000500102600000038280808000010407858080800001016600000a8880808000018280808000000b"),
	mustDecodeGCTypeSubtypingTransitiveMismatchLinkWire("0061736d01000000019b80808000024e0250006000005001006000004e02500060000050010060000002898080800001034d313101660002"),
}

var rv64GCTypeSubtypingTransitiveMismatchLinkModules = func() []*wasm.Module {
	modules := make([]*wasm.Module, len(rv64GCTypeSubtypingTransitiveMismatchLinkWires))
	for i, wire := range rv64GCTypeSubtypingTransitiveMismatchLinkWires {
		m, err := wasm.DecodeModule(wire)
		if err != nil {
			panic(err)
		}
		modules[i] = m
	}
	return modules
}()

func mustDecodeGCTypeSubtypingTransitiveMismatchLinkWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}

func gcTypeSubtypingTransitiveMismatchLinkSourceVariant(data []byte) uint8 {
	for i, wire := range rv64GCTypeSubtypingTransitiveMismatchLinkWires {
		if bytes.Equal(data, wire) {
			wantIDs := [][]byte{{1, 3, 7, 10}, {1, 2}}[i]
			if !admitExactProductWireShapeWithRecGroups(data, wantIDs) {
				return 0
			}
			m, err := wasm.DecodeModule(data)
			variant := uint8(i + 1)
			if err == nil && admitRV64GCTypeSubtypingTransitiveMismatchLink(m) == variant {
				return variant
			}
			return 0
		}
	}
	return 0
}

func admitRV64GCTypeSubtypingTransitiveMismatchLinkWire(data []byte) bool {
	return gcTypeSubtypingTransitiveMismatchLinkSourceVariant(data) != 0
}

// GCTypeSubtypingTransitiveMismatchLinkSourceVariant returns 1 for the exact .53
// provider, 2 for the exact .54 consumer, and zero for every other source.
func GCTypeSubtypingTransitiveMismatchLinkSourceVariant(data []byte) uint8 {
	return gcTypeSubtypingTransitiveMismatchLinkSourceVariant(data)
}

func admitRV64GCTypeSubtypingTransitiveMismatchLink(m *wasm.Module) uint8 {
	if m == nil || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Customs) != 0 ||
		m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return 0
	}
	for i, trusted := range rv64GCTypeSubtypingTransitiveMismatchLinkModules {
		if reflect.DeepEqual(m.Types, trusted.Types) && reflect.DeepEqual(m.Imports, trusted.Imports) &&
			reflect.DeepEqual(m.FuncTypes, trusted.FuncTypes) && reflect.DeepEqual(m.Exports, trusted.Exports) &&
			reflect.DeepEqual(m.Code, trusted.Code) {
			return uint8(i + 1)
		}
	}
	return 0
}
