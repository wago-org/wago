//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var rv64GCTypeSubtypingNonFlatExportWire = mustDecodeGCTypeSubtypingNonFlatExportWire("0061736d0100000001a8808080000450006000017d5001006000017d4f01016000017d4e036000017d50006000017d5001046000017d03878080800006000102030405079f80808000060266310000026632000102663300020266340003026635000402663600050ac9808080000687808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b87808080000043000000000b")

var rv64GCTypeSubtypingNonFlatExportModule = func() *wasm.Module {
	m, err := wasm.DecodeModule(append([]byte(nil), rv64GCTypeSubtypingNonFlatExportWire...))
	if err != nil {
		panic(err)
	}
	return m
}()

func mustDecodeGCTypeSubtypingNonFlatExportWire(source string) []byte {
	wire, err := hex.DecodeString(source)
	if err != nil {
		panic(err)
	}
	return wire
}
