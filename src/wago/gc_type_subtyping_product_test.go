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

type stagedGCTypeSubtypingProductPin struct {
	Filename string
	Line     int
	Size     int
	SHA256   string
	Class    stagedGCTypeSubtypingProduct
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
	{Filename: "type-subtyping.8.wasm", Line: 74, Size: 94, SHA256: "be069a30cbb75e3ac64dffa08757e2790ab557bc3986faa3440a7de1f87a5171", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001ad80808000044e0250006000005f016400004e0250006000005f016402004e025001006000005f004e025001026000005f00038280808000010606878080800001640400d2000b0a8880808000018280808000000b"},
	{Filename: "type-subtyping.9.wasm", Line: 86, Size: 134, SHA256: "ecfb84b0d9537fb3455ad6c0bf3c5763ba57de9167fa2e8e83f50ff15a51ac08", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001d580808000044e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f05640000640200640000640200640600038280808000010606878080800001640400d2000b0a8880808000018280808000000b"},
	{Filename: "type-subtyping.11.wasm", Line: 106, Size: 84, SHA256: "4155f7562f90dc7cfa7a1994e2511da5452045eeed10786720355c28fdf27903", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f00038280808000010406878080800001640000d2000b0a8880808000018280808000000b"},
	{Filename: "type-subtyping.12.wasm", Line: 119, Size: 150, SHA256: "6d3373700cb5c07d5c8c30f3c926d20c1cba29b1a0e512db06c7e406d7f71d1b", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001df80808000054e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406004e025001066000005f000382808080000108068d8080800002640000d2000b640400d2000b0a8880808000018280808000000b"},
	{Filename: "type-subtyping.13.wasm", Line: 129, Size: 112, SHA256: "befde5eb45b4a66d036acfc4f1b69a0b8aabea9df46aa1503b7e7ee73770dd32", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001a380808000024e025000600001647050010060000164004e0250006000016470500102600001640203838080800002000106998080800004640000d2000b640200d2000b640100d2010b640300d2010b0a918080800002838080800000000b838080800000000b"},
	{Filename: "type-subtyping.14.wasm", Line: 143, Size: 172, SHA256: "a0ba3c1005b6cb73edc08222b5d896276945b0bf1f3b3ff7ef9cdb489341fe08", Class: stagedGCTypeSubtypingRefFuncGlobals, Hex: "0061736d0100000001c780808000044e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e025001026000016470500106600001640603838080800002040506b18080800008640000d2000b640200d2000b640000d2010b640200d2010b640400d2000b640600d2000b640500d2010b640700d2010b0a918080800002838080800000000b838080800000000b"},
	{Filename: "type-subtyping.20.wasm", Line: 248, Size: 122, SHA256: "47a4b6080c4c63221e32dd452fd9bc6621c915b3f113e14e46e0f2ff907280d5", Class: stagedGCTypeSubtypingRefTestSingle, Hex: "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016402004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.21.wasm", Line: 263, Size: 162, SHA256: "97afdb1a9ad042486b76ad816e78a43f933e79b985c6fd20d0658f3b69c6e022", Class: stagedGCTypeSubtypingRefTestSingle, Hex: "0061736d0100000001d980808000054e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.22.wasm", Line: 275, Size: 122, SHA256: "9b8111ee2e3fb91cc7801a63b0a5a8e97eca7b5665f7e6fed5be8a8327534213", Class: stagedGCTypeSubtypingRefTestSingle, Hex: "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.23.wasm", Line: 286, Size: 112, SHA256: "60adfeb1cae8b65d159f8c0729630c005f5b530e90d190189487ee241f30c523", Class: stagedGCTypeSubtypingRefTestSingle, Hex: "0061736d0100000001a780808000044e0250006000005f016400004e0250006000005f016402004e025001006000005f006000017f038380808000020406078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"},
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
	seen := map[stagedGCTypeSubtypingProduct]int{}
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
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
		seen[pin.Class]++
	}
	if seen[stagedGCTypeSubtypingDeclarations] != 6 || seen[stagedGCTypeSubtypingRecursiveFunctions] != 2 || seen[stagedGCTypeSubtypingRefFuncGlobals] != 6 || seen[stagedGCTypeSubtypingRefTestSingle] != 4 {
		t.Fatalf("product classes = %#v, want declarations/recursive-functions/ref.func-globals/single-ref.test = 6/2/6/4", seen)
	}
}

func TestStagedGCTypeSubtypingProductPlatformAndBoundsGate(t *testing.T) {
	data := stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[8])
	cfg := NewRuntimeConfig()
	if guardPageBuilt {
		cfg = cfg.WithBoundsChecks(BoundsChecksSignalsBased)
	} else {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCTypeSubtypingProducts = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if goruntime.GOOS != "linux" || goruntime.GOARCH != "amd64" {
		if err == nil || !strings.Contains(err.Error(), "unsupported gc/type-subtyping product staged execution on") {
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

func TestStagedGCTypeSubtypingProductRejectsWidening(t *testing.T) {
	declaration, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[0]))
	if err != nil {
		t.Fatal(err)
	}
	declaration.Exports = append(declaration.Exports, wasm.Export{Name: "x"})
	if _, err := stagedGCTypeSubtypingProductShape(declaration); err == nil {
		t.Fatal("declaration product with an export unexpectedly admitted")
	}

	functions, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[6]))
	if err != nil {
		t.Fatal(err)
	}
	functions.Code[0].BodyBytes = []byte{0x00, 0x0b}
	if _, err := stagedGCTypeSubtypingProductShape(functions); err == nil {
		t.Fatal("recursive-function product with unreachable unexpectedly admitted")
	}
	if stagedGCTypeSubtypingProductPinned(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[0]), stagedGCTypeSubtypingRecursiveFunctions) {
		t.Fatal("declaration binary matched the recursive-function product class")
	}

	globals, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[8]))
	if err != nil {
		t.Fatal(err)
	}
	globals.Globals[0].Type.Mutable = true
	if _, err := stagedGCTypeSubtypingProductShape(globals); err == nil {
		t.Fatal("mutable ref.func global unexpectedly admitted")
	}
}
