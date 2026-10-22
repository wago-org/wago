//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCTypeSubtypingRuntimeFinalityCallCastHex = "0061736d0100000001898080800002500060000060000003878080800006000101010101048580808000017001020207a18080800004056661696c310002056661696c320003056661696c330004056661696c340005098c80808000010441000b02d2000bd2010b0acb80808000068280808000000b8280808000000b8a8080800000024041011100000b0b8a8080800000024041001101000b0b8a808080000041012500fb16001a0b8a808080000041002500fb16011a0b"

const rv64GCTypeSubtypingRuntimeFinalityKeySalt uint64 = 0x9e3779b97f4a7c15

var rv64GCTypeSubtypingRuntimeFinalityCallCastWire = func() []byte {
	wire, err := hex.DecodeString(rv64GCTypeSubtypingRuntimeFinalityCallCastHex)
	if err != nil {
		panic(err)
	}
	return wire
}()

var rv64GCTypeSubtypingRuntimeFinalityCallCastModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingRuntimeFinalityCallCastWire)
	if err != nil {
		panic(err)
	}
	return m
}()
