//go:build amd64

package amd64

import (
	"encoding/hex"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A register call returning three to seven integers captured every result in a
// GPR outside RAX..R11. With three module-pinned globals (R12-R14) and the
// function's value-pinned globals holding RBP/RSI/RDI, no such register was
// left and compilation failed with "no GP register available after spilling
// optional pins". Results that do not fit now land in spill slots.
func TestRegisterCallMultiResultsUnderPinnedGlobalPressure(t *testing.T) {
	data, err := hex.DecodeString(multiResultPinnedGlobalPressureWasm)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

// multiResultPinnedGlobalPressureWasm is a reduced wasm-tools smith module:
// function 0 keeps globals 1, 3 and 5 hot inside nested loops (module-pinned),
// and function 1 starts with a call returning (i32 i32 i64 i64) before heavy
// use of globals 1-4.
const multiResultPinnedGlobalPressureWasm = "0061736d010000000108016000047f7f7e7e030403000000062f067b01fd0c000000000000000000000000000000000b7f0141000b7e0142000b7f0141000b7e0142000b7f0141000b0a8b0303cb0105017b0170017d017c017b4100030041bb9a92817b41e3faaad47c4200bf43ff03000042ffffffffffffff7e030023050440037b230504400b03000340100088fd12fd0c000000000000f87f000000000000f87f2004fd52030023052401411f41b9f0d63b42c00042000b240224022403240324007324030b41004100420042bac79dd9d8ee8598050b852402732403fd0cffffffeffffffffff0ffffffffffffff0b24000b41004101420142000b24022402240324032402bc2401bd42010b240224022403420142000bb00103027f027c027b1002d06f431e9a5012fc04419ff396cf7a441dbc93c3efb861ffb644a6139433be57f6d64464865ed1b466d8d1ab44ecfd9a1e4d66698e44000000000000f87f20032003611bab72100143ffefffffa8fd11fd0c000000000000f87f000000000000f87f2005fd52fd2100ab0d000200420103000c0024020b2402240224032403240241004100420042010b2402240224032403240224022403732403bd2404bc2401240324021a0b0a0041004100420142000b"
