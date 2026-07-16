package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type stagedGCTypeSubtypingProductClass uint8

const (
	stagedGCTypeSubtypingDeclarations stagedGCTypeSubtypingProductClass = iota + 1
	stagedGCTypeSubtypingRecursiveFunctions
)

type stagedGCTypeSubtypingProductPin struct {
	Filename string
	Line     int
	Size     int
	SHA256   string
	Class    stagedGCTypeSubtypingProductClass
	Hex      string
}

var stagedGCTypeSubtypingProductPins = []stagedGCTypeSubtypingProductPin{
	{Filename: "type-subtyping.0.wasm", Line: 7, Size: 54, SHA256: "aa9754e0665bda5f10ec77a3261759da4b462e813ecf9d0e12ec912acff996d6", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d0100000001a8808080000750005e7f005001005e7f0050005e6e0050005e63000050005e64010050005e7f015001055e7f01"},
	{Filename: "type-subtyping.1.wasm", Line: 15, Size: 65, SHA256: "ddca4046060c72d14ed416806860b0512b8e34ae2d11555ed88ff8676f6d1871", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d0100000001b3808080000650005f005001005f005001015f017f005001025f027f006300005001035f037f006400007e015001045f037f006401007e01"},
	{Filename: "type-subtyping.2.wasm", Line: 22, Size: 61, SHA256: "30ea9ab7a806640c081a4cd0bb68ecd9125f37524b6137f60af89a1c69df2839", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d0100000001af808080000650005f005001005f00500060016401016e5001026001640001646e5001036001630001640050010460016b016401"},
	{Filename: "type-subtyping.3.wasm", Line: 28, Size: 39, SHA256: "76131bcda4dc51168d7c55feabbc7bfb3489dc399b2bb3d0a89a05c56964b5cd", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d010000000199808080000350005f016e005001005f016401005001015f026401007f00"},
	{Filename: "type-subtyping.4.wasm", Line: 34, Size: 46, SHA256: "2be8c2ca40f321f5ab956b191184d9b988e1f81963704f316f506bf18235bc9b", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d0100000001a0808080000250005f027f006400004e025001005f027f006402005001005f027f00640100"},
	{Filename: "type-subtyping.5.wasm", Line: 42, Size: 73, SHA256: "ad59582ba55bea406e6c3f6a473bb1fbef90e66275bec4848972483b302ac8c9", Class: stagedGCTypeSubtypingDeclarations, Hex: "0061736d0100000001bb80808000024e0250005f027f0064010050005f027e006400004e035001015f037e006400007f005001005f037f006401007f005001015f037e006403007f00"},
	{Filename: "type-subtyping.6.wasm", Line: 53, Size: 120, SHA256: "6c5162870907b88c444e61528fe907f280fb2b38b8877bbe98ed58bfebddd496", Class: stagedGCTypeSubtypingRecursiveFunctions, Hex: "0061736d0100000001ac80808000044e03500060027f64020050010060027f64010050010160027f640000600164000060016401006001640200038480808000030304050aae8080800003868080800000200010000b8a808080000020001000200010010b8e80808000002000100020001001200010020b"},
	{Filename: "type-subtyping.7.wasm", Line: 65, Size: 144, SHA256: "7421ec51f0e574ac1248b32bc37a7cc0a93445ccf58879e757def2af49039e3a", Class: stagedGCTypeSubtypingRecursiveFunctions, Hex: "0061736d0100000001c880808000054e0250006000027f640150006000027d64004e045001006000027f64055001016000027d64045001006000027f64035001016000027d6402600164000060016402006001640400038480808000030607080aaa8080800003868080800000200010000b8a808080000020001000200010010b8a808080000020001000200010020b"},
}

func stagedGCTypeSubtypingProductData(t testing.TB, pin stagedGCTypeSubtypingProductPin) []byte {
	t.Helper()
	data, err := hex.DecodeString(pin.Hex)
	if err != nil {
		t.Fatalf("%s hex: %v", pin.Filename, err)
	}
	return data
}

func TestStagedGCTypeSubtypingProductInventory(t *testing.T) {
	seen := map[stagedGCTypeSubtypingProductClass]int{}
	for _, pin := range stagedGCTypeSubtypingProductPins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		if len(data) != pin.Size {
			t.Fatalf("%s size = %d, want %d", pin.Filename, len(data), pin.Size)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != pin.SHA256 {
			t.Fatalf("%s sha256 = %s, want %s", pin.Filename, got, pin.SHA256)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatalf("%s decode: %v", pin.Filename, err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatalf("%s validate: %v", pin.Filename, err)
		}
		seen[pin.Class]++
	}
	if seen[stagedGCTypeSubtypingDeclarations] != 6 || seen[stagedGCTypeSubtypingRecursiveFunctions] != 2 {
		t.Fatalf("product classes = %#v, want declarations/recursive-functions = 6/2", seen)
	}
}
