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
	Results  []uint64
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
	{Filename: "type-subtyping.20.wasm", Line: 248, Size: 122, SHA256: "47a4b6080c4c63221e32dd452fd9bc6621c915b3f113e14e46e0f2ff907280d5", Class: stagedGCTypeSubtypingRefTestSingle, Results: []uint64{1}, Hex: "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016402004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.21.wasm", Line: 263, Size: 162, SHA256: "97afdb1a9ad042486b76ad816e78a43f933e79b985c6fd20d0658f3b69c6e022", Class: stagedGCTypeSubtypingRefTestSingle, Results: []uint64{1}, Hex: "0061736d0100000001d980808000054e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.22.wasm", Line: 275, Size: 122, SHA256: "9b8111ee2e3fb91cc7801a63b0a5a8e97eca7b5665f7e6fed5be8a8327534213", Class: stagedGCTypeSubtypingRefTestSingle, Results: []uint64{0}, Hex: "0061736d0100000001b180808000054e0250006000005f016400004e0250006000005f016400004e025001006000005f004e025001026000005f006000017f038380808000020608078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14040b"},
	{Filename: "type-subtyping.23.wasm", Line: 286, Size: 112, SHA256: "60adfeb1cae8b65d159f8c0729630c005f5b530e90d190189487ee241f30c523", Class: stagedGCTypeSubtypingRefTestSingle, Results: []uint64{1}, Hex: "0061736d0100000001a780808000044e0250006000005f016400004e0250006000005f016402004e025001006000005f006000017f038380808000020406078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"},
	{Filename: "type-subtyping.24.wasm", Line: 302, Size: 178, SHA256: "5f080674a00a73b3dba391bb1967aa22f4dd6f1b43b9b49aff08528c3305aa6b", Class: stagedGCTypeSubtypingRefTestMulti, Results: []uint64{1, 1}, Hex: "0061736d0100000001e480808000064e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f056400006400006402006402006404004e025001026000005001035f056400006402006400006402006406004e025001066000005f006000027f7f03838080800002080a078780808000010372756e000109858080800001030001000a9980808000028280808000000b8c8080800000d200fb1400d200fb14040b"},
	{Filename: "type-subtyping.25.wasm", Line: 315, Size: 144, SHA256: "b561b7bcd131223f573b787ff002cec3ef83d1cb90fc440ec24d347cc789df1d", Class: stagedGCTypeSubtypingRefTestMulti, Results: []uint64{1, 1, 1, 1}, Hex: "0061736d0100000001aa80808000034e025000600001647050010060000164004e025000600001647050010260000164026000047f7f7f7f03848080800003000104078780808000010372756e00020989808080000203000100030001010aac8080800003838080800000000b838080800000000b968080800000d200fb1400d200fb1402d201fb1401d201fb14030b"},
	{Filename: "type-subtyping.26.wasm", Line: 338, Size: 204, SHA256: "893dcf058c5b28436567028ab41bfb409c5f1acc737e764a3dfcc51f6be8200e", Class: stagedGCTypeSubtypingRefTestMulti, Results: []uint64{1, 1, 1, 1, 1, 1, 1, 1}, Hex: "0061736d0100000001d280808000054e025000600001647050010060000164004e025000600001647050010260000164024e02500100600001647050010460000164044e02500102600001647050010660000164066000087f7f7f7f7f7f7f7f03848080800003040508078780808000010372756e00020989808080000203000100030001010ac08080800003838080800000000b838080800000000baa8080800000d200fb1400d200fb1402d201fb1400d201fb1402d200fb1404d200fb1406d201fb1405d201fb14070b"},
	{Filename: "type-subtyping.27.wasm", Line: 359, Size: 104, SHA256: "2841d098dfca125ccd9c577cf55762744c8a3911a1986f857be48ebc0d51f735", Class: stagedGCTypeSubtypingRefTestDirectionFalse, Results: []uint64{0}, Hex: "0061736d01000000019f80808000034e0250006000005001006000004e0250006000005001006000006000017f038380808000020204078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14000b"},
	{Filename: "type-subtyping.28.wasm", Line: 371, Size: 117, SHA256: "b0797a1825d04be467e336f7f236637184aab41a13de20ff7a06eb1bb7885613", Class: stagedGCTypeSubtypingRefTestDirectionFalse, Results: []uint64{0}, Hex: "0061736d0100000001ac80808000044e0250006000005001006000004e0250006000005001006000004e0250006000005001026000006000017f038380808000020406078780808000010372756e000109858080800001030001000a9480808000028280808000000b878080800000d200fb14020b"},
	{Filename: "type-subtyping.17.wasm", Line: 193, Size: 412, SHA256: "505e94dbd66fc2e3b5d2d4af76341618b19571074c7b42a551392fd58aa692f3", Class: stagedGCTypeSubtypingRuntimeCallCast, Hex: "0061736d01000000019a808080000450006000017050010060000163015001016000016302600000038b808080000a00010203030303030303048580808000017001030307b780808000070372756e0003056661696c310004056661696c320005056661696c330006056661696c340007056661696c350008056661696c360009098f80808000010441000b03d2000bd2010bd2020b0a80828080000a848080800000d0700b848080800000d0010b848080800000d0020bf98080800000027041001100000b027041011100000b027041021100000b02630141011101000b02630141021101000b02630241021102000b02630041002500fb16000b02630041012500fb16000b02630041022500fb16000b02630141012500fb16010b02630141022500fb16010b02630241022500fb16020b0c000b8d808080000002630141001101000b0c000b8d808080000002630141001102000b0c000b8d808080000002630141011102000b0c000b8b808080000041002500fb16010c000b8b808080000041002500fb16020c000b8b808080000041012500fb16020c000b"},
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
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
		if len(pin.Results) != 0 {
			runner := len(m.Code) - 1
			ft, ok := m.ResolvedLocalFuncType(runner)
			if !ok || len(ft.Results) != len(pin.Results) {
				t.Fatalf("%s runner results = %v, want %d ordered i32 results", pin.Filename, ft.Results, len(pin.Results))
			}
			for _, result := range ft.Results {
				if !wasm.EqualValType(result, wasm.I32) {
					t.Fatalf("%s runner result = %v, want i32", pin.Filename, result)
				}
			}
		}
		seen[pin.Class]++
	}
	if seen[stagedGCTypeSubtypingDeclarations] != 6 || seen[stagedGCTypeSubtypingRecursiveFunctions] != 2 || seen[stagedGCTypeSubtypingRefFuncGlobals] != 6 || seen[stagedGCTypeSubtypingRefTestSingle] != 4 || seen[stagedGCTypeSubtypingRefTestMulti] != 3 || seen[stagedGCTypeSubtypingRefTestDirectionFalse] != 2 || seen[stagedGCTypeSubtypingRuntimeCallCast] != 1 {
		t.Fatalf("product classes = %#v, want declarations/recursive-functions/ref.func-globals/single-ref.test/multi-ref.test/direction-false-ref.test/runtime-call-cast = 6/2/6/4/3/2/1", seen)
	}
}

func TestStagedGCTypeSubtypingMultiRefTestInventory(t *testing.T) {
	wantFuncCounts := []int{2, 3, 3}
	wantElemFuncs := [][][]uint32{{{0}}, {{0}, {1}}, {{0}, {1}}}
	wantBodies := []string{
		"d200fb1400d200fb14040b",
		"d200fb1400d200fb1402d201fb1401d201fb14030b",
		"d200fb1400d200fb1402d201fb1400d201fb1402d200fb1404d200fb1406d201fb1405d201fb14070b",
	}
	for i, pin := range stagedGCTypeSubtypingProductPins[18:21] {
		m, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, pin))
		if err != nil {
			t.Fatalf("%s decode: %v", pin.Filename, err)
		}
		if len(m.FuncTypes) != wantFuncCounts[i] || len(m.Code) != wantFuncCounts[i] {
			t.Fatalf("%s functions/code = %d/%d, want %d/%d", pin.Filename, len(m.FuncTypes), len(m.Code), wantFuncCounts[i], wantFuncCounts[i])
		}
		if len(m.Elements) != len(wantElemFuncs[i]) {
			t.Fatalf("%s elements = %d, want %d", pin.Filename, len(m.Elements), len(wantElemFuncs[i]))
		}
		for j, want := range wantElemFuncs[i] {
			got := m.Elements[j]
			matches := len(got.Kind.Funcs) == len(want)
			for k := range got.Kind.Funcs {
				matches = matches && uint32(got.Kind.Funcs[k]) == want[k]
			}
			if got.Mode.Kind != wasm.ElemDeclarative || got.Kind.Kind != wasm.ElemFuncs || !matches {
				t.Fatalf("%s element %d = mode %v kind %v funcs %v, want declarative funcs %v", pin.Filename, j, got.Mode.Kind, got.Kind.Kind, got.Kind.Funcs, want)
			}
		}
		for j := 0; j < len(m.Code)-1; j++ {
			wantBody := "0b"
			if len(m.Code) == 3 {
				wantBody = "000b"
			}
			if got := hex.EncodeToString(m.Code[j].BodyBytes); got != wantBody {
				t.Fatalf("%s function %d body = %s, want %s", pin.Filename, j, got, wantBody)
			}
		}
		if got := hex.EncodeToString(m.Code[len(m.Code)-1].BodyBytes); got != wantBodies[i] {
			t.Fatalf("%s runner body = %s, want %s", pin.Filename, got, wantBodies[i])
		}
	}
}

func TestStagedGCTypeSubtypingDirectionFalseRefTestInventory(t *testing.T) {
	wantFuncTypes := [][2]uint32{{2, 4}, {4, 6}}
	wantTargets := []uint32{0, 2}
	wantTargetSecondSupers := []wasm.TypeIdx{{Index: 0, Rec: true}, {Index: 0}}
	wantBodies := []string{"d200fb14000b", "d200fb14020b"}
	for i, pin := range stagedGCTypeSubtypingProductPins[21:23] {
		m, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, pin))
		if err != nil {
			t.Fatalf("%s decode: %v", pin.Filename, err)
		}
		if len(m.FuncTypes) != 2 || m.FuncTypes[0].Index != wantFuncTypes[i][0] || m.FuncTypes[1].Index != wantFuncTypes[i][1] {
			t.Fatalf("%s function types = %v, want source/runner indexes %v", pin.Filename, m.FuncTypes, wantFuncTypes[i])
		}
		if got := hex.EncodeToString(m.Code[0].BodyBytes); got != "0b" {
			t.Fatalf("%s source body = %s, want empty", pin.Filename, got)
		}
		if got := hex.EncodeToString(m.Code[1].BodyBytes); got != wantBodies[i] {
			t.Fatalf("%s runner body = %s, want %s", pin.Filename, got, wantBodies[i])
		}
		pairs, ok := exactRefFuncTestBody(m.Code[1].BodyBytes)
		if !ok || len(pairs) != 1 || pairs[0].funcIndex != 0 || pairs[0].targetType != wantTargets[i] {
			t.Fatalf("%s pair = %+v, %v; want local function 0 tested against type %d", pin.Filename, pairs, ok, wantTargets[i])
		}
		sourceGroup := int(m.FuncTypes[0].Index / 2)
		targetGroup := int(wantTargets[i] / 2)
		if sourceGroup >= len(m.Types) || targetGroup >= len(m.Types) || len(m.Types[sourceGroup].SubTypes) != 2 || len(m.Types[targetGroup].SubTypes) != 2 {
			t.Fatalf("%s source/target recursive groups are not exact two-member groups", pin.Filename)
		}
		for _, groupIndex := range []int{targetGroup, sourceGroup} {
			for memberIndex, subtype := range m.Types[groupIndex].SubTypes {
				if subtype.Final || !subtype.HasPrefix || subtype.Comp.Kind != wasm.CompFunc || len(subtype.Comp.Params) != 0 || len(subtype.Comp.Results) != 0 {
					t.Fatalf("%s group %d member %d = %+v, want open empty function subtype", pin.Filename, groupIndex, memberIndex, subtype)
				}
			}
			if len(m.Types[groupIndex].SubTypes[0].Supers) != 0 {
				t.Fatalf("%s group %d source member unexpectedly has supers %v", pin.Filename, groupIndex, m.Types[groupIndex].SubTypes[0].Supers)
			}
		}
		sourceSecond := m.Types[sourceGroup].SubTypes[1]
		if len(sourceSecond.Supers) != 1 || sourceSecond.Supers[0].Rec || sourceSecond.Supers[0].Index != wantTargets[i] {
			t.Fatalf("%s source-group second super = %v, want absolute target type %d", pin.Filename, sourceSecond.Supers, wantTargets[i])
		}
		targetSecond := m.Types[targetGroup].SubTypes[1]
		if len(targetSecond.Supers) != 1 || targetSecond.Supers[0] != wantTargetSecondSupers[i] {
			t.Fatalf("%s target-group second super = %v, want %v", pin.Filename, targetSecond.Supers, wantTargetSecondSupers[i])
		}
		actual := wasm.Ref(false, wasm.IndexedHeap(m.FuncTypes[0]), false)
		required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: wantTargets[i]}), false)
		if m.ReferenceTypeSubtype(actual, required) {
			t.Fatalf("%s source type %d unexpectedly subtypes target type %d; false direction must remain exact", pin.Filename, m.FuncTypes[0].Index, wantTargets[i])
		}
	}
}

func TestStagedGCTypeSubtypingRuntimeCallCastInventory(t *testing.T) {
	pin := stagedGCTypeSubtypingProductPins[23]
	m, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, pin))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Types) != 4 || len(m.FuncTypes) != 10 || len(m.Code) != 10 || len(m.Tables) != 1 || len(m.Elements) != 1 || len(m.Exports) != 7 {
		t.Fatalf("runtime call/cast shape types/functions/code/tables/elements/exports = %d/%d/%d/%d/%d/%d, want 4/10/10/1/1/7", len(m.Types), len(m.FuncTypes), len(m.Code), len(m.Tables), len(m.Elements), len(m.Exports))
	}
	for i := 0; i < 3; i++ {
		st := m.Types[i].SubTypes[0]
		if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 1 {
			t.Fatalf("type %d = %+v, want open zero-parameter single-reference-result function", i, st)
		}
		if i == 0 && len(st.Supers) != 0 || i > 0 && (len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(i-1)) {
			t.Fatalf("type %d supers = %v, want exact chain", i, st.Supers)
		}
	}
	for source := uint32(0); source < 3; source++ {
		for target := uint32(0); target < 3; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if got, want := m.ReferenceTypeSubtype(actual, required), source >= target; got != want {
				t.Fatalf("function subtype %d <: %d = %v, want %v", source, target, got, want)
			}
		}
	}
	table := m.Tables[0].Type
	if !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || table.Limits.Min != 3 || table.Limits.Max == nil || *table.Limits.Max != 3 || table.Limits.Addr64 {
		t.Fatalf("table = %+v, want exact table 3 3 funcref", table)
	}
	elem := m.Elements[0]
	if elem.Mode.Kind != wasm.ElemActive || elem.Mode.Table != 0 || !isExactI32ConstZeroBody(elem.Mode.Offset.BodyBytes) || elem.Kind.Kind != wasm.ElemFuncExprs || len(elem.Kind.Exprs) != 3 {
		t.Fatalf("element = %+v, want active table-0 offset-0 three-function expressions", elem)
	}
	for i := range elem.Kind.Exprs {
		if !isExactRefFuncBody(elem.Kind.Exprs[i].BodyBytes, uint32(i)) {
			t.Fatalf("element expression %d = %x, want ref.func %d", i, elem.Kind.Exprs[i].BodyBytes, i)
		}
	}
	wantTypes := []uint32{0, 1, 2, 3, 3, 3, 3, 3, 3, 3}
	wantExports := []string{"run", "fail1", "fail2", "fail3", "fail4", "fail5", "fail6"}
	for i := range m.FuncTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != wantTypes[i] {
			t.Fatalf("function %d type = %v, want %d", i, m.FuncTypes[i], wantTypes[i])
		}
	}
	for i := range m.Exports {
		if m.Exports[i].Name != wantExports[i] || m.Exports[i].Index.Kind != wasm.ExternFunc || m.Exports[i].Index.Index != uint32(i+3) {
			t.Fatalf("export %d = %+v, want %s function %d", i, m.Exports[i], wantExports[i], i+3)
		}
	}
	wantBodies := []string{
		"d0700b", "d0010b", "d0020b",
		"027041001100000b027041011100000b027041021100000b02630141011101000b02630141021101000b02630241021102000b02630041002500fb16000b02630041012500fb16000b02630041022500fb16000b02630141012500fb16010b02630141022500fb16010b02630241022500fb16020b0c000b",
		"02630141001101000b0c000b", "02630141001102000b0c000b", "02630141011102000b0c000b",
		"41002500fb16010c000b", "41002500fb16020c000b", "41012500fb16020c000b",
	}
	for i := range m.Code {
		if len(m.Code[i].Locals.Runs) != 0 || hex.EncodeToString(m.Code[i].BodyBytes) != wantBodies[i] {
			t.Fatalf("function %d locals/body = %v/%x, want none/%s", i, m.Code[i].Locals.Runs, m.Code[i].BodyBytes, wantBodies[i])
		}
	}
}

func TestStagedGCTypeSubtypingProductPlatformAndBoundsGate(t *testing.T) {
	for _, pinIndex := range []int{8, 14, 18, 21, 23} {
		pin := stagedGCTypeSubtypingProductPins[pinIndex]
		t.Run(pin.Filename, func(t *testing.T) {
			data := stagedGCTypeSubtypingProductData(t, pin)
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
		})
	}
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

	refTest, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[14]))
	if err != nil {
		t.Fatal(err)
	}
	refTest.Code[1].BodyBytes = []byte{0xd2, 0x00, 0x1a, 0x0b}
	if _, err := stagedGCTypeSubtypingProductShape(refTest); err == nil {
		t.Fatal("single ref.test product with drop instead of ref.test unexpectedly admitted")
	}

	multi, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[18]))
	if err != nil {
		t.Fatal(err)
	}
	multi.Elements[0].Kind.Funcs[0] = 1
	if _, err := stagedGCTypeSubtypingProductShape(multi); err == nil {
		t.Fatal("multi-result ref.test product with widened element identity unexpectedly admitted")
	}

	directionFalse, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[21]))
	if err != nil {
		t.Fatal(err)
	}
	directionFalse.Types[1].SubTypes[1].Supers[0] = wasm.TypeIdx{Index: 2, Rec: true}
	if product, err := stagedGCTypeSubtypingProductShape(directionFalse); err == nil && product == stagedGCTypeSubtypingRefTestDirectionFalse {
		t.Fatal("direction-false ref.test product with a widened source-group super relation unexpectedly retained exact admission")
	}

	runtimeCallCast, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[23]))
	if err != nil {
		t.Fatal(err)
	}
	max := uint64(4)
	runtimeCallCast.Tables[0].Type.Limits.Max = &max
	if product, err := stagedGCTypeSubtypingProductShape(runtimeCallCast); err == nil && product == stagedGCTypeSubtypingRuntimeCallCast {
		t.Fatal("runtime call/cast product with widened table maximum unexpectedly retained exact admission")
	}
}
