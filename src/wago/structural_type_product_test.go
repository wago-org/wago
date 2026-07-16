package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type stagedTypeRecLeaderPin struct {
	Filename string
	Line     int
	Size     int
	SHA256   string
	Product  stagedStructuralTypeProduct
	Hex      string
}

var stagedTypeRecLeaderPins = []stagedTypeRecLeaderPin{
	{Filename: "type-rec.3.wasm", Line: 29, Size: 57, SHA256: "537a62d99f8643a7b0dcc1fb73514b847e1ff9b19bbc0a6c70ef9f63569e914f", Product: stagedStructuralRefFuncGlobal, Hex: "0061736d01000000018880808000014e026000005f00038280808000010006878080800001640000d2000b0a8880808000018280808000000b"},
	{Filename: "type-rec.7.wasm", Line: 62, Size: 70, SHA256: "20f1e69dfad585cb943f18cff49bfdeee48a141aa85186ce12cb07d286894d39", Product: stagedStructuralRefFuncGlobal, Hex: "0061736d01000000019580808000024e026000005f016400004e026000005f01640200038280808000010206878080800001640000d2000b0a8880808000018280808000000b"},
	{Filename: "type-rec.8.wasm", Line: 73, Size: 114, SHA256: "86770dc27154217df11c4e7dcbbce07e592c7f1915c71fa2987496821169846f", Product: stagedStructuralRefFuncGlobal, Hex: "0061736d0100000001c180808000044e026000005f016400004e026000005f016402004e026000005f056400006400006402006402006404004e026000005f05640000640200640000640200640600038280808000010606878080800001640400d2000b0a8880808000018280808000000b"},
	{Filename: "type-rec.13.wasm", Line: 119, Size: 55, SHA256: "06b03a6d32fb8f85b7d9f89d73c6e4d02a556faefdbe8875cdd30d2c839db327", Product: stagedStructuralFunctionLink, Hex: "0061736d01000000018880808000014e026000005f00038280808000010007858080800001016600000a8880808000018280808000000b"},
	{Filename: "type-rec.14.wasm", Line: 126, Size: 35, SHA256: "8880e1366eedcb1bbff51008184f9c619c50a442c897e861139b4f5e9e8c5948", Product: stagedStructuralFunctionLink, Hex: "0061736d01000000018880808000014e026000005f0002878080800001014d01660000"},
	{Filename: "type-rec.15.wasm", Line: 132, Size: 35, SHA256: "efd00ff8bd9cf9f29eafd3b8bbeefd83d2fa48e7e9efc6821ad55f028cdd93f8", Product: stagedStructuralFunctionLink, Hex: "0061736d01000000018880808000014e025f0060000002878080800001014d01660001"},
	{Filename: "type-rec.17.wasm", Line: 147, Size: 106, SHA256: "1f64997e12f4531cdc52825acd5d7964ebf962117127b8ceeb55faefdf0a82be", Product: stagedStructuralCallIndirect, Hex: "0061736d01000000019280808000034e026000005f004e026000005f006000000383808080000200040485808080000170010101078780808000010372756e0001098980808000010441000b01d2000b0a9480808000028280808000000b87808080000041001102000b"},
	{Filename: "type-rec.18.wasm", Line: 158, Size: 106, SHA256: "51eea44e5eb322b92f16cc9fb27c856c16e6459c89aef172e981092f5561037b", Product: stagedStructuralCallIndirect, Hex: "0061736d01000000019280808000034e026000005f004e025f006000006000000383808080000200040485808080000170010101078780808000010372756e0001098980808000010441000b01d2000b0a9480808000028280808000000b87808080000041001103000b"},
	{Filename: "type-rec.19.wasm", Line: 169, Size: 99, SHA256: "06e932f89a548c23e504276ab4892e468dc0c188ec7d0b80e2185eb79b50b898", Product: stagedStructuralCallIndirect, Hex: "0061736d01000000018b80808000024e026000005f006000000383808080000200020485808080000170010101078780808000010372756e0001098980808000010441000b01d2000b0a9480808000028280808000000b87808080000041001102000b"},
	{Filename: "type-rec.20.wasm", Line: 177, Size: 57, SHA256: "aaa094e7f9c9510fc710e7710ac1972ebd727e29ae8f506e6a8b2634c0eb9729", Product: stagedStructuralRefFuncGlobal, Hex: "0061736d01000000018880808000025f006001640000038280808000010106878080800001640100d2000b0a8880808000018280808000000b"},
}

func stagedTypeRecLeaderPinFor(data []byte, line int) (stagedTypeRecLeaderPin, bool) {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, pin := range stagedTypeRecLeaderPins {
		if pin.Line == line && pin.Size == len(data) && pin.SHA256 == sum {
			return pin, true
		}
	}
	return stagedTypeRecLeaderPin{}, false
}

func TestStagedTypeRecLeaderInventory(t *testing.T) {
	seen := map[stagedStructuralTypeProduct]int{}
	for _, pin := range stagedTypeRecLeaderPins {
		data, err := hex.DecodeString(pin.Hex)
		if err != nil {
			t.Fatalf("%s hex: %v", pin.Filename, err)
		}
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
		got, err := stagedStructuralTypeProductShape(m)
		if err != nil || got != pin.Product {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, got, err, pin.Product)
		}
		if !stagedStructuralTypeProductPinned(data, got) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
		seen[got]++
	}
	if seen[stagedStructuralRefFuncGlobal] != 4 || seen[stagedStructuralFunctionLink] != 3 || seen[stagedStructuralCallIndirect] != 3 {
		t.Fatalf("leader classes = %#v, want ref.func/link/call_indirect = 4/3/3", seen)
	}
}

func compileStagedStructuralTypeProductForTest(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.StructuralTypeProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedStructuralTypeProductPlatformAndBoundsGate(t *testing.T) {
	data, err := hex.DecodeString(stagedTypeRecLeaderPins[0].Hex)
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.StructuralTypeProducts = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported collector-free structural product staged execution on") {
			t.Fatalf("platform compile = %v, want explicit platform rejection", err)
		}
		return
	}
	if guardPageBuilt {
		if err == nil || !strings.Contains(err.Error(), "signals-based bounds checks") {
			t.Fatalf("guard compile = %v, want explicit bounds rejection", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("linux/amd64 explicit compile: %v", err)
	}
	_ = c.Close()
}

func TestStagedStructuralTypeProductRejectsWidening(t *testing.T) {
	data, err := hex.DecodeString(stagedTypeRecLeaderPins[0].Hex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	start := wasm.FuncIdx(0)
	m.Start = &start
	if _, err := stagedStructuralTypeProductShape(m); err == nil {
		t.Fatal("structural product with start unexpectedly admitted")
	}
	m.Start = nil
	m.Types[0].SubTypes[1].Comp.Fields = append(m.Types[0].SubTypes[1].Comp.Fields, wasm.FieldType{Mut: 1})
	if _, err := stagedStructuralTypeProductShape(m); err == nil {
		t.Fatal("structural product with mutable struct field unexpectedly admitted")
	}
}
