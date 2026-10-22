//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRuntimeTypedTableCallHex = "0061736d01000000019580808000045000600000500100600000500101600000600000038680808000050102030303048680808000016301010202079780808000030372756e0002056661696c310003056661696c320004098f8080800001060041000b630102d2000bd2010b0ac780808000058280808000000b8280808000000b9b8080800000410011000041011100004100110100410111010041011102000b87808080000041001102000b87808080000041001103000b"

var rv64GCTypeSubtypingRuntimeTypedTableCallWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRuntimeTypedTableCallHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRuntimeTypedTableCallModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRuntimeTypedTableCallWire)
	if err != nil {
		panic(err)
	}
	return m
}()

func rv64GCTypeSubtypingRuntimeTypedTableKey(typeIndex uint32, structural uint64) uint64 {

	return structural ^ uint64(typeIndex)*rv64GCTypeSubtypingRuntimeFinalityKeySalt
}
