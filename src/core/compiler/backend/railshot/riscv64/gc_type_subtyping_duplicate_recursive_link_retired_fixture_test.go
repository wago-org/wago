//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
