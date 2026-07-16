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

var stagedGCTypeSubtypingTypedTablePin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.19.wasm",
	Line:     234,
	Size:     186,
	SHA256:   "2ad95457821ceb8211d5733fe308f031f1103755733bbf8b5db9c85db0eb6d9b",
	Class:    stagedGCTypeSubtypingRuntimeTypedTableCall,
	Hex:      "0061736d01000000019580808000045000600000500100600000500101600000600000038680808000050102030303048680808000016301010202079780808000030372756e0002056661696c310003056661696c320004098f8080800001060041000b630102d2000bd2010b0ac780808000058280808000000b8280808000000b9b8080800000410011000041011100004100110100410111010041011102000b87808080000041001102000b87808080000041001103000b",
}

var stagedGCTypeSubtypingLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.29.wasm",
	Line:     382,
	Size:     103,
	SHA256:   "8e9bdbeb27a496328eb9529e0ea629d14a01124b657c4eb0aad74a8bd0f426db",
	Class:    stagedGCTypeSubtypingLinkProvider,
	Hex:      "0061736d01000000019780808000035000600001705001006000016301500101600001630203848080800003000102079080808000030266300000026631000102663200020a9c8080800003848080800000d0700b848080800000d0010b848080800000d0020b",
}

var stagedGCTypeSubtypingLinkConsumerPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.30.wasm",
	Line:     392,
	Size:     86,
	SHA256:   "ea4d5aaf13a9744bd319a1b33d1ee2303cfaecc84dae73a4179351a6fb91a760",
	Class:    stagedGCTypeSubtypingLinkConsumer,
	Hex:      "0061736d01000000019780808000035000600001705001006000016301500101600001630202ab8080800006014d0266300000014d0266310000014d0266310001014d0266320000014d0266320001014d0266320002",
}

var stagedGCTypeSubtypingLinkUnlinkablePins = []stagedGCTypeSubtypingProductPin{
	{Filename: "type-subtyping.31.wasm", Line: 399, Size: 51, SHA256: "634f7caa3c4e26b757fca7a9a9f8367f99e33304c87f7b2cf6ec7d1e31566535", Class: stagedGCTypeSubtypingLinkConsumer, Hex: "0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300001"},
	{Filename: "type-subtyping.32.wasm", Line: 406, Size: 51, SHA256: "fe07228154a27a9de4702afb12187709536e894495e8fa2e34a710e2dd7c0b88", Class: stagedGCTypeSubtypingLinkConsumer, Hex: "0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266300002"},
	{Filename: "type-subtyping.33.wasm", Line: 413, Size: 51, SHA256: "24ce2b2eec631ee2946c641e0545d06dc1179e2e9ba646ae59c5d37974111649", Class: stagedGCTypeSubtypingLinkConsumer, Hex: "0061736d01000000019780808000035000600001705001006000016301500101600001630202888080800001014d0266310002"},
}

var stagedGCTypeSubtypingFinalityLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.34.wasm",
	Line:     421,
	Size:     70,
	SHA256:   "dcf54459e9f39087c697c9d9edc0955aabc02eb28e40b65c84291cbe194a9562",
	Class:    stagedGCTypeSubtypingFinalityLinkProvider,
	Hex:      "0061736d01000000018980808000025000600000600000038380808000020001078b8080800002026631000002663200010a8f80808000028280808000000b8280808000000b",
}

var stagedGCTypeSubtypingFinalityLinkUnlinkablePins = []stagedGCTypeSubtypingProductPin{
	{Filename: "type-subtyping.35.wasm", Line: 428, Size: 38, SHA256: "ea960ddec4f24c952d26ee7a567309a41c5895cf84690ca120d4577bb4c26e08", Class: stagedGCTypeSubtypingFinalityLinkConsumer, Hex: "0061736d0100000001898080800002500060000060000002898080800001024d320266310001"},
	{Filename: "type-subtyping.36.wasm", Line: 434, Size: 38, SHA256: "7fc43bbbff42ca923db1604d0339cadd21458f5671ea7962d031786e93517996", Class: stagedGCTypeSubtypingFinalityLinkConsumer, Hex: "0061736d0100000001898080800002500060000060000002898080800001024d320266320000"},
}

var stagedGCTypeSubtypingStructLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.37.wasm",
	Line:     442,
	Size:     70,
	SHA256:   "ac63802e3827e33389d92ff8a8bd25b6231f1dde96bab5cb77a0e1d094f80e6f",
	Class:    stagedGCTypeSubtypingStructLinkProvider,
	Hex:      "0061736d01000000019780808000024e0250006000005f016400004e025001006000005f00038280808000010207858080800001016700000a8880808000018280808000000b",
}

var stagedGCTypeSubtypingStructLinkConsumerPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.38.wasm",
	Line:     450,
	Size:     51,
	SHA256:   "5f090989edc62437b56b36c69a316cdcfddec4a63d451bd9443ad59da75af0a3",
	Class:    stagedGCTypeSubtypingStructLinkConsumer,
	Hex:      "0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3301670002",
}

var stagedGCTypeSubtypingStructProjectionLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.39.wasm",
	Line:     460,
	Size:     104,
	SHA256:   "8de41fdb1e1b4ef57639e5a6344eed6c13bfb5ada5ea56433bb221f403c56d8e",
	Class:    stagedGCTypeSubtypingStructProjectionLinkProvider,
	Hex:      "0061736d0100000001b980808000034e02500060000050005f016400004e02500060000050005f016402004e025001026000005001035f05640000640200640000640200640400038280808000010407858080800001016700000a8880808000018280808000000b",
}

var stagedGCTypeSubtypingStructProjectionLinkConsumerPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.40.wasm",
	Line:     470,
	Size:     85,
	SHA256:   "a5d3e6060f52fa0becf68e6e4dd06623df6ecf7bf22bfe5430b484f2adbdf0a2",
	Class:    stagedGCTypeSubtypingStructProjectionLinkConsumer,
	Hex:      "0061736d0100000001b980808000034e02500060000050005f016400004e02500060000050005f016402004e025001006000005001015f0564000064000064020064020064040002888080800001024d3401670004",
}

var stagedGCTypeSubtypingStructMismatchLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.41.wasm",
	Line:     479,
	Size:     82,
	SHA256:   "0494d7c95b50e151ac8e0f9eb8a1c935a016db45b1969378ed95d40369fda062",
	Class:    stagedGCTypeSubtypingStructMismatchLinkProvider,
	Hex:      "0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016400004e025001026000005f00038280808000010407858080800001016700000a8880808000018280808000000b",
}

var stagedGCTypeSubtypingStructMismatchLinkConsumerPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.42.wasm",
	Line:     487,
	Size:     51,
	SHA256:   "bb598cc89f2d73720190e6c7e115bec104013bf8ebead4c417d17e701598c7a1",
	Class:    stagedGCTypeSubtypingStructMismatchLinkConsumer,
	Hex:      "0061736d01000000019780808000024e0250006000005f016400004e025001006000005f0002888080800001024d3501670002",
}

var stagedGCTypeSubtypingIndependentStructLinkProviderPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.43.wasm",
	Line:     496,
	Size:     82,
	SHA256:   "7c8af0765c2e2d43a07e7a6a75a85d396531827c1b2cb4402a24277308781dff",
	Class:    stagedGCTypeSubtypingIndependentStructLinkProvider,
	Hex:      "0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f00038280808000010407858080800001016700000a8880808000018280808000000b",
}

var stagedGCTypeSubtypingIndependentStructLinkConsumerPin = stagedGCTypeSubtypingProductPin{
	Filename: "type-subtyping.44.wasm",
	Line:     504,
	Size:     63,
	SHA256:   "a593d0db0e5f173aaac2d6007b84a4b268d7ad2047a4e8cd8fe3a275ef9b0820",
	Class:    stagedGCTypeSubtypingIndependentStructLinkConsumer,
	Hex:      "0061736d0100000001a380808000034e0250006000005f016400004e0250006000005f016402004e025001006000005f0002888080800001024d3601670000",
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
	{Filename: "type-subtyping.18.wasm", Line: 215, Size: 185, SHA256: "375a327f8469d41d4f15f05109533a90127fc5287414364e227203d7d48e7662", Class: stagedGCTypeSubtypingRuntimeFinalityCallCast, Hex: "0061736d0100000001898080800002500060000060000003878080800006000101010101048580808000017001020207a18080800004056661696c310002056661696c320003056661696c330004056661696c340005098c80808000010441000b02d2000bd2010b0acb80808000068280808000000b8280808000000b8a8080800000024041011100000b0b8a8080800000024041001101000b0b8a808080000041012500fb16001a0b8a808080000041002500fb16011a0b"},
	stagedGCTypeSubtypingTypedTablePin,
	stagedGCTypeSubtypingLinkProviderPin,
	stagedGCTypeSubtypingLinkConsumerPin,
	stagedGCTypeSubtypingFinalityLinkProviderPin,
	stagedGCTypeSubtypingStructLinkProviderPin,
	stagedGCTypeSubtypingStructLinkConsumerPin,
	stagedGCTypeSubtypingStructProjectionLinkProviderPin,
	stagedGCTypeSubtypingStructProjectionLinkConsumerPin,
	stagedGCTypeSubtypingStructMismatchLinkProviderPin,
	stagedGCTypeSubtypingStructMismatchLinkConsumerPin,
	stagedGCTypeSubtypingIndependentStructLinkProviderPin,
	stagedGCTypeSubtypingIndependentStructLinkConsumerPin,
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
	if seen[stagedGCTypeSubtypingDeclarations] != 6 || seen[stagedGCTypeSubtypingRecursiveFunctions] != 2 || seen[stagedGCTypeSubtypingRefFuncGlobals] != 6 || seen[stagedGCTypeSubtypingRefTestSingle] != 4 || seen[stagedGCTypeSubtypingRefTestMulti] != 3 || seen[stagedGCTypeSubtypingRefTestDirectionFalse] != 2 || seen[stagedGCTypeSubtypingRuntimeCallCast] != 1 || seen[stagedGCTypeSubtypingRuntimeFinalityCallCast] != 1 || seen[stagedGCTypeSubtypingRuntimeTypedTableCall] != 1 || seen[stagedGCTypeSubtypingLinkProvider] != 1 || seen[stagedGCTypeSubtypingLinkConsumer] != 1 || seen[stagedGCTypeSubtypingFinalityLinkProvider] != 1 || seen[stagedGCTypeSubtypingStructLinkProvider] != 1 || seen[stagedGCTypeSubtypingStructLinkConsumer] != 1 || seen[stagedGCTypeSubtypingStructProjectionLinkProvider] != 1 || seen[stagedGCTypeSubtypingStructProjectionLinkConsumer] != 1 || seen[stagedGCTypeSubtypingStructMismatchLinkProvider] != 1 || seen[stagedGCTypeSubtypingStructMismatchLinkConsumer] != 1 || seen[stagedGCTypeSubtypingIndependentStructLinkProvider] != 1 || seen[stagedGCTypeSubtypingIndependentStructLinkConsumer] != 1 {
		t.Fatalf("product classes = %#v, want declarations/recursive-functions/ref.func-globals/single-ref.test/multi-ref.test/direction-false-ref.test/runtime-call-cast/runtime-finality-call-cast/runtime-typed-table-call/link-provider/link-consumer/finality-link-provider/struct-link-provider/struct-link-consumer/struct-projection-provider/struct-projection-consumer/struct-mismatch-provider/struct-mismatch-consumer/independent-struct-provider/independent-struct-consumer = 6/2/6/4/3/2/1/1/1/1/1/1/1/1/1/1/1/1/1/1", seen)
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

func TestStagedGCTypeSubtypingRuntimeFinalityCallCastInventory(t *testing.T) {
	pin := stagedGCTypeSubtypingProductPins[24]
	m, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, pin))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Types) != 2 || len(m.FuncTypes) != 6 || len(m.Code) != 6 || len(m.Tables) != 1 || len(m.Elements) != 1 || len(m.Exports) != 4 {
		t.Fatalf("runtime finality shape types/functions/code/tables/elements/exports = %d/%d/%d/%d/%d/%d, want 2/6/6/1/1/4", len(m.Types), len(m.FuncTypes), len(m.Code), len(m.Tables), len(m.Elements), len(m.Exports))
	}
	open := m.Types[0].SubTypes[0]
	if open.Final || !open.HasPrefix || len(open.Supers) != 0 || open.Comp.Kind != wasm.CompFunc || len(open.Comp.Params) != 0 || len(open.Comp.Results) != 0 {
		t.Fatalf("open type = %+v, want open () -> () subtype", open)
	}
	final := m.Types[1].SubTypes[0]
	if !final.Final || final.HasPrefix || len(final.Supers) != 0 || final.Comp.Kind != wasm.CompFunc || len(final.Comp.Params) != 0 || len(final.Comp.Results) != 0 {
		t.Fatalf("final type = %+v, want final () -> () type", final)
	}
	for source := uint32(0); source < 2; source++ {
		for target := uint32(0); target < 2; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if got, want := m.ReferenceTypeSubtype(actual, required), source == target; got != want {
				t.Fatalf("finality-sensitive function subtype %d <: %d = %v, want %v", source, target, got, want)
			}
		}
	}
	table := m.Tables[0].Type
	if !wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) || table.Limits.Min != 2 || table.Limits.Max == nil || *table.Limits.Max != 2 || table.Limits.Addr64 {
		t.Fatalf("table = %+v, want exact table 2 2 funcref", table)
	}
	elem := m.Elements[0]
	if elem.Mode.Kind != wasm.ElemActive || elem.Mode.Table != 0 || !isExactI32ConstZeroBody(elem.Mode.Offset.BodyBytes) || elem.Kind.Kind != wasm.ElemFuncExprs || len(elem.Kind.Exprs) != 2 {
		t.Fatalf("element = %+v, want active table-0 offset-0 two-function expressions", elem)
	}
	for i := range elem.Kind.Exprs {
		if !isExactRefFuncBody(elem.Kind.Exprs[i].BodyBytes, uint32(i)) {
			t.Fatalf("element expression %d = %x, want ref.func %d", i, elem.Kind.Exprs[i].BodyBytes, i)
		}
	}
	wantTypes := []uint32{0, 1, 1, 1, 1, 1}
	wantExports := []string{"fail1", "fail2", "fail3", "fail4"}
	for i := range m.FuncTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != wantTypes[i] {
			t.Fatalf("function %d type = %v, want %d", i, m.FuncTypes[i], wantTypes[i])
		}
	}
	for i := range m.Exports {
		if m.Exports[i].Name != wantExports[i] || m.Exports[i].Index.Kind != wasm.ExternFunc || m.Exports[i].Index.Index != uint32(i+2) {
			t.Fatalf("export %d = %+v, want %s function %d", i, m.Exports[i], wantExports[i], i+2)
		}
	}
	wantBodies := []string{
		"0b", "0b",
		"024041011100000b0b", "024041001101000b0b",
		"41012500fb16001a0b", "41002500fb16011a0b",
	}
	for i := range m.Code {
		if len(m.Code[i].Locals.Runs) != 0 || hex.EncodeToString(m.Code[i].BodyBytes) != wantBodies[i] {
			t.Fatalf("function %d locals/body = %v/%x, want none/%s", i, m.Code[i].Locals.Runs, m.Code[i].BodyBytes, wantBodies[i])
		}
	}
}

func TestStagedGCTypeSubtypingRuntimeTypedTableInventory(t *testing.T) {
	pin := stagedGCTypeSubtypingTypedTablePin
	data := stagedGCTypeSubtypingProductData(t, pin)
	if len(data) != pin.Size {
		t.Fatalf("typed-table size = %d, want %d", len(data), pin.Size)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != pin.SHA256 {
		t.Fatalf("typed-table sha256 = %s, want %s", got, pin.SHA256)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("AST validate: %v", err)
	}
	if err := wasm.ValidateByteBackedModule(data); err != nil {
		t.Fatalf("byte-backed validate: %v", err)
	}
	if len(m.Types) != 4 || len(m.FuncTypes) != 5 || len(m.Code) != 5 || len(m.Tables) != 1 || len(m.Elements) != 1 || len(m.Exports) != 3 {
		t.Fatalf("typed-table shape types/functions/code/tables/elements/exports = %d/%d/%d/%d/%d/%d, want 4/5/5/1/1/3", len(m.Types), len(m.FuncTypes), len(m.Code), len(m.Tables), len(m.Elements), len(m.Exports))
	}
	for i := 0; i < 3; i++ {
		if len(m.Types[i].SubTypes) != 1 {
			t.Fatalf("typed-table group %d members = %d, want 1", i, len(m.Types[i].SubTypes))
		}
		st := m.Types[i].SubTypes[0]
		if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 0 {
			t.Fatalf("typed-table type %d = %+v, want open () -> () subtype", i, st)
		}
		if i == 0 && len(st.Supers) != 0 || i > 0 && (len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(i-1)) {
			t.Fatalf("typed-table type %d supers = %v, want exact chain", i, st.Supers)
		}
	}
	runner := m.Types[3].SubTypes[0]
	if !runner.Final || runner.HasPrefix || len(runner.Supers) != 0 || runner.Comp.Kind != wasm.CompFunc || len(runner.Comp.Params) != 0 || len(runner.Comp.Results) != 0 {
		t.Fatalf("typed-table runner type = %+v, want final () -> ()", runner)
	}
	for source := uint32(0); source < 3; source++ {
		for target := uint32(0); target < 3; target++ {
			actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
			required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
			if got, want := m.ReferenceTypeSubtype(actual, required), source >= target; got != want {
				t.Fatalf("typed-table function subtype %d <: %d = %v, want %v", source, target, got, want)
			}
		}
	}
	table := m.Tables[0].Type
	wantTableType := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	if !wasm.EqualValType(wasm.RefVal(table.Ref), wantTableType) || table.Limits.Addr64 || table.Limits.Min != 2 || table.Limits.Max == nil || *table.Limits.Max != 2 || m.Tables[0].Init != nil {
		t.Fatalf("typed table = %+v, want exact table 2 2 (ref null type 1)", table)
	}
	for _, source := range []uint32{1, 2} {
		actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
		if !m.ReferenceTypeSubtype(actual, table.Ref) {
			t.Fatalf("typed-table element source type %d is not storable in table type 1", source)
		}
	}
	if m.ReferenceTypeSubtype(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false), table.Ref) {
		t.Fatal("typed-table source type 0 unexpectedly storable in narrower table type 1")
	}
	elem := m.Elements[0]
	if elem.Mode.Kind != wasm.ElemActive || elem.Mode.Table != 0 || !isExactI32ConstZeroBody(elem.Mode.Offset.BodyBytes) || elem.Kind.Kind != wasm.ElemTypedExprs || !wasm.EqualValType(wasm.RefVal(elem.Kind.Ref), wantTableType) || len(elem.Kind.Exprs) != 2 {
		t.Fatalf("typed-table element = %+v, want active table-0 offset-0 two typed function expressions", elem)
	}
	for i := range elem.Kind.Exprs {
		if !isExactRefFuncBody(elem.Kind.Exprs[i].BodyBytes, uint32(i)) {
			t.Fatalf("typed-table element expression %d = %x, want ref.func %d", i, elem.Kind.Exprs[i].BodyBytes, i)
		}
	}
	wantTypes := []uint32{1, 2, 3, 3, 3}
	wantExports := []string{"run", "fail1", "fail2"}
	for i, want := range wantTypes {
		if m.FuncTypes[i].Rec || m.FuncTypes[i].Index != want || len(m.Code[i].Locals.Runs) != 0 {
			t.Fatalf("typed-table function %d type/locals = %v/%v, want type %d/no locals", i, m.FuncTypes[i], m.Code[i].Locals.Runs, want)
		}
	}
	for i, want := range wantExports {
		if m.Exports[i].Name != want || m.Exports[i].Index.Kind != wasm.ExternFunc || m.Exports[i].Index.Index != uint32(i+2) {
			t.Fatalf("typed-table export %d = %+v, want %s function %d", i, m.Exports[i], want, i+2)
		}
	}
	wantBodies := []string{
		"0b", "0b",
		"410011000041011100004100110100410111010041011102000b",
		"41001102000b", "41001103000b",
	}
	for i, want := range wantBodies {
		if got := hex.EncodeToString(m.Code[i].BodyBytes); got != want {
			t.Fatalf("typed-table function %d body = %s, want %s", i, got, want)
		}
	}
	if !stagedGCTypeSubtypingProductPinned(data, pin.Class) {
		t.Fatal("typed-table binary is not in the production SHA pin set")
	}
	if product, err := stagedGCTypeSubtypingProductShape(m); err != nil || product != pin.Class {
		t.Fatalf("typed-table product = %v, %v; want %v", product, err, pin.Class)
	}
}

func TestStagedGCTypeSubtypingFirstLinkingClusterInventory(t *testing.T) {
	pins := append([]stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingLinkProviderPin, stagedGCTypeSubtypingLinkConsumerPin}, stagedGCTypeSubtypingLinkUnlinkablePins...)
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		if len(m.Types) != 3 {
			t.Fatalf("%s type groups = %d, want 3", pin.Filename, len(m.Types))
		}
		for typeIndex := 0; typeIndex < 3; typeIndex++ {
			if len(m.Types[typeIndex].SubTypes) != 1 {
				t.Fatalf("%s type group %d members = %d, want 1", pin.Filename, typeIndex, len(m.Types[typeIndex].SubTypes))
			}
			st := m.Types[typeIndex].SubTypes[0]
			if st.Final || !st.HasPrefix || st.Comp.Kind != wasm.CompFunc || len(st.Comp.Params) != 0 || len(st.Comp.Results) != 1 {
				t.Fatalf("%s type %d = %+v, want open zero-parameter single-reference-result function", pin.Filename, typeIndex, st)
			}
			if typeIndex == 0 {
				if len(st.Supers) != 0 || !wasm.EqualValType(st.Comp.Results[0], wasm.FuncRef) {
					t.Fatalf("%s root type = %+v, want () -> funcref without super", pin.Filename, st)
				}
			} else {
				result := st.Comp.Results[0]
				if len(st.Supers) != 1 || st.Supers[0].Rec || st.Supers[0].Index != uint32(typeIndex-1) || result.Kind != wasm.ValRef || !result.Ref.Nullable || result.Ref.Heap.Kind != wasm.HeapTypeIndex || !result.Ref.Heap.Type.Rec || result.Ref.Heap.Type.Index != 0 {
					t.Fatalf("%s type %d = %+v, want exact recursive subtype/result chain", pin.Filename, typeIndex, st)
				}
			}
		}
		for source := uint32(0); source < 3; source++ {
			for target := uint32(0); target < 3; target++ {
				actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
				required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
				if got, want := m.ReferenceTypeSubtype(actual, required), source >= target; got != want {
					t.Fatalf("%s function subtype %d <: %d = %v, want %v", pin.Filename, source, target, got, want)
				}
			}
		}
	}

	provider := modules[0]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 3 || len(provider.Code) != 3 || len(provider.Exports) != 3 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/3/3/3 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	wantBodies := []string{"d0700b", "d0010b", "d0020b"}
	for i := 0; i < 3; i++ {
		if provider.FuncTypes[i].Rec || provider.FuncTypes[i].Index != uint32(i) || len(provider.Code[i].Locals.Runs) != 0 || hex.EncodeToString(provider.Code[i].BodyBytes) != wantBodies[i] {
			t.Fatalf("provider function %d type/locals/body = %v/%v/%x, want type %d/no locals/%s", i, provider.FuncTypes[i], provider.Code[i].Locals.Runs, provider.Code[i].BodyBytes, i, wantBodies[i])
		}
		wantName := fmt.Sprintf("f%d", i)
		if provider.Exports[i].Name != wantName || provider.Exports[i].Index.Kind != wasm.ExternFunc || provider.Exports[i].Index.Index != uint32(i) {
			t.Fatalf("provider export %d = %+v, want %s function %d", i, provider.Exports[i], wantName, i)
		}
	}

	consumer := modules[1]
	wantImports := []struct {
		name      string
		typeIndex uint32
	}{{"f0", 0}, {"f1", 0}, {"f1", 1}, {"f2", 0}, {"f2", 1}, {"f2", 2}}
	if len(consumer.Imports) != len(wantImports) || consumer.ImportedFuncCount() != len(wantImports) || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
		t.Fatalf("consumer imports/code/exports = %d/%d/%d, want 6/0/0", len(consumer.Imports), len(consumer.Code), len(consumer.Exports))
	}
	for i, want := range wantImports {
		imp := consumer.Imports[i]
		if imp.Module != "M" || imp.Name != want.name || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != want.typeIndex {
			t.Fatalf("consumer import %d = %+v, want M.%s type %d", i, imp, want.name, want.typeIndex)
		}
	}

	wantRejected := []struct {
		name      string
		typeIndex uint32
	}{{"f0", 1}, {"f0", 2}, {"f1", 2}}
	for i, want := range wantRejected {
		m := modules[i+2]
		if len(m.Imports) != 1 || m.ImportedFuncCount() != 1 || len(m.Code) != 0 || len(m.Exports) != 0 {
			t.Fatalf("%s state imports/code/exports = %d/%d/%d, want 1/0/0", pins[i+2].Filename, len(m.Imports), len(m.Code), len(m.Exports))
		}
		imp := m.Imports[0]
		if imp.Module != "M" || imp.Name != want.name || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != want.typeIndex {
			t.Fatalf("%s import = %+v, want M.%s type %d", pins[i+2].Filename, imp, want.name, want.typeIndex)
		}
	}

	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingFinalityLinkingClusterInventory(t *testing.T) {
	pins := append([]stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingFinalityLinkProviderPin}, stagedGCTypeSubtypingFinalityLinkUnlinkablePins...)
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		if len(m.Types) != 2 || len(m.Types[0].SubTypes) != 1 || len(m.Types[1].SubTypes) != 1 {
			t.Fatalf("%s type groups/members = %d/%d/%d, want 2/1/1", pin.Filename, len(m.Types), len(m.Types[0].SubTypes), len(m.Types[1].SubTypes))
		}
		open := m.Types[0].SubTypes[0]
		if open.Final || !open.HasPrefix || len(open.Supers) != 0 || open.Comp.Kind != wasm.CompFunc || len(open.Comp.Params) != 0 || len(open.Comp.Results) != 0 {
			t.Fatalf("%s open type = %+v, want open () -> ()", pin.Filename, open)
		}
		final := m.Types[1].SubTypes[0]
		if !final.Final || final.HasPrefix || len(final.Supers) != 0 || final.Comp.Kind != wasm.CompFunc || len(final.Comp.Params) != 0 || len(final.Comp.Results) != 0 {
			t.Fatalf("%s final type = %+v, want final () -> ()", pin.Filename, final)
		}
		for source := uint32(0); source < 2; source++ {
			for target := uint32(0); target < 2; target++ {
				actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: source}), false)
				required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: target}), false)
				if got, want := m.ReferenceTypeSubtype(actual, required), source == target; got != want {
					t.Fatalf("%s finality relation %d <: %d = %v, want %v", pin.Filename, source, target, got, want)
				}
			}
		}
	}

	provider := modules[0]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 2 || len(provider.Code) != 2 || len(provider.Exports) != 2 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/2/2/2 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	for i, wantName := range []string{"f1", "f2"} {
		if provider.FuncTypes[i].Rec || provider.FuncTypes[i].Index != uint32(i) || len(provider.Code[i].Locals.Runs) != 0 || !isExactEndBody(provider.Code[i].BodyBytes) {
			t.Fatalf("provider function %d type/locals/body = %v/%v/%x, want type %d/no locals/empty", i, provider.FuncTypes[i], provider.Code[i].Locals.Runs, provider.Code[i].BodyBytes, i)
		}
		if provider.Exports[i].Name != wantName || provider.Exports[i].Index.Kind != wasm.ExternFunc || provider.Exports[i].Index.Index != uint32(i) {
			t.Fatalf("provider export %d = %+v, want %s function %d", i, provider.Exports[i], wantName, i)
		}
	}

	wantRejected := []struct {
		name      string
		typeIndex uint32
	}{{"f1", 1}, {"f2", 0}}
	for i, want := range wantRejected {
		consumer := modules[i+1]
		if len(consumer.Imports) != 1 || consumer.ImportedFuncCount() != 1 || len(consumer.FuncTypes) != 0 || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
			t.Fatalf("%s state imports/functions/code/exports = %d/%d/%d/%d, want 1/0/0/0", pins[i+1].Filename, len(consumer.Imports), len(consumer.FuncTypes), len(consumer.Code), len(consumer.Exports))
		}
		imp := consumer.Imports[0]
		if imp.Module != "M2" || imp.Name != want.name || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != want.typeIndex {
			t.Fatalf("%s import = %+v, want M2.%s type %d", pins[i+1].Filename, imp, want.name, want.typeIndex)
		}
	}

	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingStructLinkingClusterInventory(t *testing.T) {
	pins := []stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingStructLinkProviderPin, stagedGCTypeSubtypingStructLinkConsumerPin}
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		if len(m.Types) != 2 || len(m.Types[0].SubTypes) != 2 || len(m.Types[1].SubTypes) != 2 {
			t.Fatalf("%s type groups/members = %d/%d/%d, want 2/2/2", pin.Filename, len(m.Types), len(m.Types[0].SubTypes), len(m.Types[1].SubTypes))
		}
		f := m.Types[0].SubTypes[0]
		if f.Final || !f.HasPrefix || len(f.Supers) != 0 || f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
			t.Fatalf("%s first-group function = %+v, want open () -> () root", pin.Filename, f)
		}
		s := m.Types[0].SubTypes[1]
		if !s.Final || s.HasPrefix || len(s.Supers) != 0 || s.Comp.Kind != wasm.CompStruct || len(s.Comp.Fields) != 1 {
			t.Fatalf("%s first-group struct = %+v, want final one-field struct", pin.Filename, s)
		}
		field := s.Comp.Fields[0]
		ref := field.Storage.Val
		if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || !ref.Ref.Heap.Type.Rec || ref.Ref.Heap.Type.Index != 0 {
			t.Fatalf("%s struct field = %+v, want immutable non-null reference to recursive member 0", pin.Filename, field)
		}
		g := m.Types[1].SubTypes[0]
		if g.Final || !g.HasPrefix || len(g.Supers) != 1 || g.Supers[0].Rec || g.Supers[0].Index != 0 || g.Comp.Kind != wasm.CompFunc || len(g.Comp.Params) != 0 || len(g.Comp.Results) != 0 {
			t.Fatalf("%s second-group function = %+v, want open () -> () subtype of flat type 0", pin.Filename, g)
		}
		empty := m.Types[1].SubTypes[1]
		if !empty.Final || empty.HasPrefix || len(empty.Supers) != 0 || empty.Comp.Kind != wasm.CompStruct || len(empty.Comp.Fields) != 0 {
			t.Fatalf("%s second-group struct = %+v, want final empty struct", pin.Filename, empty)
		}
		for gi := range m.Types {
			for si := range m.Types[gi].SubTypes {
				st := m.Types[gi].SubTypes[si]
				if st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
					t.Fatalf("%s type group/member %d/%d carries descriptor metadata", pin.Filename, gi, si)
				}
			}
		}
		gRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false)
		fRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
		if !m.ReferenceTypeSubtype(gRef, fRef) || m.ReferenceTypeSubtype(fRef, gRef) {
			t.Fatalf("%s function relation g <: f / f <: g = %v/%v, want true/false", pin.Filename, m.ReferenceTypeSubtype(gRef, fRef), m.ReferenceTypeSubtype(fRef, gRef))
		}
	}

	provider := modules[0]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 1 || len(provider.Code) != 1 || len(provider.Exports) != 1 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/1/1/1 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	if provider.FuncTypes[0].Rec || provider.FuncTypes[0].Index != 2 || len(provider.Code[0].Locals.Runs) != 0 || !isExactEndBody(provider.Code[0].BodyBytes) {
		t.Fatalf("provider function type/locals/body = %v/%v/%x, want flat type 2/no locals/empty", provider.FuncTypes[0], provider.Code[0].Locals.Runs, provider.Code[0].BodyBytes)
	}
	if provider.Exports[0].Name != "g" || provider.Exports[0].Index.Kind != wasm.ExternFunc || provider.Exports[0].Index.Index != 0 {
		t.Fatalf("provider export = %+v, want g function 0", provider.Exports[0])
	}

	consumer := modules[1]
	if len(consumer.Imports) != 1 || consumer.ImportedFuncCount() != 1 || len(consumer.FuncTypes) != 0 || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
		t.Fatalf("consumer imports/functions/code/exports = %d/%d/%d/%d, want 1/0/0/0", len(consumer.Imports), len(consumer.FuncTypes), len(consumer.Code), len(consumer.Exports))
	}
	imp := consumer.Imports[0]
	if imp.Module != "M3" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 2 {
		t.Fatalf("consumer import = %+v, want M3.g flat type 2", imp)
	}
	providerTypes, err := typeDescriptorsFromWasm(provider)
	if err != nil {
		t.Fatalf("provider descriptors: %v", err)
	}
	consumerTypes, err := typeDescriptorsFromWasm(consumer)
	if err != nil {
		t.Fatalf("consumer descriptors: %v", err)
	}
	actual := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 2}}
	required := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 2}}
	if !referenceTypeSubtype(actual, providerTypes, required, consumerTypes) {
		t.Fatal("provider g type is not a structural subtype of consumer M3.g requirement")
	}

	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingStructProjectionLinkingClusterInventory(t *testing.T) {
	pins := []stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingStructProjectionLinkProviderPin, stagedGCTypeSubtypingStructProjectionLinkConsumerPin}
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		if len(m.Types) != 3 {
			t.Fatalf("%s type groups = %d, want 3", pin.Filename, len(m.Types))
		}
		for groupIndex := range m.Types {
			group := m.Types[groupIndex]
			if len(group.SubTypes) != 2 {
				t.Fatalf("%s group %d members = %d, want 2", pin.Filename, groupIndex, len(group.SubTypes))
			}
			for memberIndex := range group.SubTypes {
				st := group.SubTypes[memberIndex]
				if st.Final || !st.HasPrefix || st.Metadata.Describes != nil || st.Metadata.Descriptor != nil {
					t.Fatalf("%s group/member %d/%d = %+v, want open metadata-free subtype", pin.Filename, groupIndex, memberIndex, st)
				}
			}
			f := group.SubTypes[0]
			if f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
				t.Fatalf("%s group %d function = %+v, want () -> ()", pin.Filename, groupIndex, f)
			}
			s := group.SubTypes[1]
			wantFields := 1
			if groupIndex == 2 {
				wantFields = 5
			}
			if s.Comp.Kind != wasm.CompStruct || len(s.Comp.Fields) != wantFields {
				t.Fatalf("%s group %d struct = %+v, want %d fields", pin.Filename, groupIndex, s, wantFields)
			}
		}
		for groupIndex := 0; groupIndex < 2; groupIndex++ {
			for memberIndex := 0; memberIndex < 2; memberIndex++ {
				if supers := m.Types[groupIndex].SubTypes[memberIndex].Supers; len(supers) != 0 {
					t.Fatalf("%s root group/member %d/%d supers = %v, want none", pin.Filename, groupIndex, memberIndex, supers)
				}
			}
			field := m.Types[groupIndex].SubTypes[1].Comp.Fields[0]
			ref := field.Storage.Val
			if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || !ref.Ref.Heap.Type.Rec || ref.Ref.Heap.Type.Index != 0 {
				t.Fatalf("%s root group %d field = %+v, want immutable non-null recursive function member 0", pin.Filename, groupIndex, field)
			}
		}
		wantSuper := uint32(2)
		wantStructSuper := uint32(3)
		wantFields := []wasm.TypeIdx{{Index: 0}, {Index: 2}, {Index: 0}, {Index: 2}, {Index: 0, Rec: true}}
		if i == 1 {
			wantSuper = 0
			wantStructSuper = 1
			wantFields = []wasm.TypeIdx{{Index: 0}, {Index: 0}, {Index: 2}, {Index: 2}, {Index: 0, Rec: true}}
		}
		last := m.Types[2]
		if supers := last.SubTypes[0].Supers; len(supers) != 1 || supers[0].Rec || supers[0].Index != wantSuper {
			t.Fatalf("%s projected function supers = %v, want flat type %d", pin.Filename, supers, wantSuper)
		}
		if supers := last.SubTypes[1].Supers; len(supers) != 1 || supers[0].Rec || supers[0].Index != wantStructSuper {
			t.Fatalf("%s projected struct supers = %v, want flat type %d", pin.Filename, supers, wantStructSuper)
		}
		for fieldIndex, want := range wantFields {
			field := last.SubTypes[1].Comp.Fields[fieldIndex]
			ref := field.Storage.Val
			if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || ref.Ref.Heap.Type != want {
				t.Fatalf("%s projected struct field %d = %+v, want immutable non-null %v", pin.Filename, fieldIndex, field, want)
			}
		}
	}

	provider, consumer := modules[0], modules[1]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 1 || len(provider.Code) != 1 || len(provider.Exports) != 1 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/1/1/1 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	if provider.FuncTypes[0].Rec || provider.FuncTypes[0].Index != 4 || len(provider.Code[0].Locals.Runs) != 0 || !isExactEndBody(provider.Code[0].BodyBytes) {
		t.Fatalf("provider function type/locals/body = %v/%v/%x, want flat type 4/no locals/empty", provider.FuncTypes[0], provider.Code[0].Locals.Runs, provider.Code[0].BodyBytes)
	}
	if provider.Exports[0].Name != "g" || provider.Exports[0].Index.Kind != wasm.ExternFunc || provider.Exports[0].Index.Index != 0 {
		t.Fatalf("provider export = %+v, want g function 0", provider.Exports[0])
	}
	if len(consumer.Imports) != 1 || consumer.ImportedFuncCount() != 1 || len(consumer.FuncTypes) != 0 || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
		t.Fatalf("consumer imports/functions/code/exports = %d/%d/%d/%d, want 1/0/0/0", len(consumer.Imports), len(consumer.FuncTypes), len(consumer.Code), len(consumer.Exports))
	}
	imp := consumer.Imports[0]
	if imp.Module != "M4" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 4 {
		t.Fatalf("consumer import = %+v, want M4.g flat type 4", imp)
	}
	providerTypes, err := typeDescriptorsFromWasm(provider)
	if err != nil {
		t.Fatalf("provider descriptors: %v", err)
	}
	consumerTypes, err := typeDescriptorsFromWasm(consumer)
	if err != nil {
		t.Fatalf("consumer descriptors: %v", err)
	}
	actual := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 4}}
	required := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 4}}
	if !referenceTypeSubtype(actual, providerTypes, required, consumerTypes) {
		t.Fatal("provider M4.g source type is not a structural subtype of the consumer requirement")
	}
	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingStructMismatchLinkingClusterInventory(t *testing.T) {
	pins := []stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingStructMismatchLinkProviderPin, stagedGCTypeSubtypingStructMismatchLinkConsumerPin}
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		wantGroups := 3
		if i == 1 {
			wantGroups = 2
		}
		if len(m.Types) != wantGroups {
			t.Fatalf("%s type groups = %d, want %d", pin.Filename, len(m.Types), wantGroups)
		}
		for groupIndex := range m.Types {
			group := m.Types[groupIndex]
			if len(group.SubTypes) != 2 {
				t.Fatalf("%s group %d members = %d, want 2", pin.Filename, groupIndex, len(group.SubTypes))
			}
			f := group.SubTypes[0]
			if f.Final || !f.HasPrefix || f.Metadata.Describes != nil || f.Metadata.Descriptor != nil || f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
				t.Fatalf("%s group %d function = %+v, want open metadata-free () -> ()", pin.Filename, groupIndex, f)
			}
			s := group.SubTypes[1]
			if !s.Final || s.HasPrefix || s.Metadata.Describes != nil || s.Metadata.Descriptor != nil || s.Comp.Kind != wasm.CompStruct {
				t.Fatalf("%s group %d struct = %+v, want final metadata-free struct", pin.Filename, groupIndex, s)
			}
			if groupIndex == wantGroups-1 {
				wantSuper := wasm.TypeIdx{Index: uint32(2 * (groupIndex - 1))}
				if len(f.Supers) != 1 || f.Supers[0] != wantSuper {
					t.Fatalf("%s group %d function supers = %v, want [%v]", pin.Filename, groupIndex, f.Supers, wantSuper)
				}
			} else if len(f.Supers) != 0 {
				t.Fatalf("%s group %d function supers = %v, want none", pin.Filename, groupIndex, f.Supers)
			}
			if len(s.Supers) != 0 {
				t.Fatalf("%s group %d struct supers = %v, want none", pin.Filename, groupIndex, s.Supers)
			}
			if groupIndex == wantGroups-1 {
				if len(s.Comp.Fields) != 0 {
					t.Fatalf("%s final group struct fields = %v, want empty", pin.Filename, s.Comp.Fields)
				}
				continue
			}
			if len(s.Comp.Fields) != 1 {
				t.Fatalf("%s root group %d struct fields = %d, want 1", pin.Filename, groupIndex, len(s.Comp.Fields))
			}
			field := s.Comp.Fields[0]
			ref := field.Storage.Val
			want := wasm.TypeIdx{Index: 0, Rec: groupIndex == 0}
			if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || ref.Ref.Heap.Type != want {
				t.Fatalf("%s root group %d field = %+v, want immutable non-null reference %v", pin.Filename, groupIndex, field, want)
			}
		}
		gIndex := uint32(2 * (wantGroups - 1))
		fIndex := gIndex - 2
		gRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: gIndex}), false)
		fRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: fIndex}), false)
		if !m.ReferenceTypeSubtype(gRef, fRef) || m.ReferenceTypeSubtype(fRef, gRef) {
			t.Fatalf("%s function relation g <: f / f <: g = %v/%v, want true/false", pin.Filename, m.ReferenceTypeSubtype(gRef, fRef), m.ReferenceTypeSubtype(fRef, gRef))
		}
	}

	provider, consumer := modules[0], modules[1]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 1 || len(provider.Code) != 1 || len(provider.Exports) != 1 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/1/1/1 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	if provider.FuncTypes[0].Rec || provider.FuncTypes[0].Index != 4 || len(provider.Code[0].Locals.Runs) != 0 || !isExactEndBody(provider.Code[0].BodyBytes) {
		t.Fatalf("provider function type/locals/body = %v/%v/%x, want flat type 4/no locals/empty", provider.FuncTypes[0], provider.Code[0].Locals.Runs, provider.Code[0].BodyBytes)
	}
	if provider.Exports[0].Name != "g" || provider.Exports[0].Index.Kind != wasm.ExternFunc || provider.Exports[0].Index.Index != 0 {
		t.Fatalf("provider export = %+v, want g function 0", provider.Exports[0])
	}
	if len(consumer.Imports) != 1 || consumer.ImportedFuncCount() != 1 || len(consumer.FuncTypes) != 0 || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
		t.Fatalf("consumer imports/functions/code/exports = %d/%d/%d/%d, want 1/0/0/0", len(consumer.Imports), len(consumer.FuncTypes), len(consumer.Code), len(consumer.Exports))
	}
	imp := consumer.Imports[0]
	if imp.Module != "M5" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 2 {
		t.Fatalf("consumer import = %+v, want M5.g flat type 2", imp)
	}
	providerTypes, err := typeDescriptorsFromWasm(provider)
	if err != nil {
		t.Fatalf("provider descriptors: %v", err)
	}
	consumerTypes, err := typeDescriptorsFromWasm(consumer)
	if err != nil {
		t.Fatalf("consumer descriptors: %v", err)
	}
	actual := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 4}}
	required := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 2}}
	if referenceTypeSubtype(actual, providerTypes, required, consumerTypes) {
		t.Fatal("provider M5.g source type unexpectedly satisfies the consumer requirement; recursive group identity was flattened")
	}
	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingIndependentStructLinkingClusterInventory(t *testing.T) {
	pins := []stagedGCTypeSubtypingProductPin{stagedGCTypeSubtypingIndependentStructLinkProviderPin, stagedGCTypeSubtypingIndependentStructLinkConsumerPin}
	modules := make([]*wasm.Module, len(pins))
	for i, pin := range pins {
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
			t.Fatalf("%s AST validate: %v", pin.Filename, err)
		}
		if err := wasm.ValidateByteBackedModule(data); err != nil {
			t.Fatalf("%s byte-backed validate: %v", pin.Filename, err)
		}
		modules[i] = m
		if len(m.Types) != 3 {
			t.Fatalf("%s type groups = %d, want 3", pin.Filename, len(m.Types))
		}
		for groupIndex := range m.Types {
			group := m.Types[groupIndex]
			if len(group.SubTypes) != 2 {
				t.Fatalf("%s group %d members = %d, want 2", pin.Filename, groupIndex, len(group.SubTypes))
			}
			f := group.SubTypes[0]
			if f.Final || !f.HasPrefix || f.Metadata.Describes != nil || f.Metadata.Descriptor != nil || f.Comp.Kind != wasm.CompFunc || len(f.Comp.Params) != 0 || len(f.Comp.Results) != 0 {
				t.Fatalf("%s group %d function = %+v, want open metadata-free () -> ()", pin.Filename, groupIndex, f)
			}
			s := group.SubTypes[1]
			if !s.Final || s.HasPrefix || s.Metadata.Describes != nil || s.Metadata.Descriptor != nil || len(s.Supers) != 0 || s.Comp.Kind != wasm.CompStruct {
				t.Fatalf("%s group %d struct = %+v, want final metadata-free struct without supers", pin.Filename, groupIndex, s)
			}
			if groupIndex < 2 {
				if len(f.Supers) != 0 || len(s.Comp.Fields) != 1 {
					t.Fatalf("%s root group %d supers/fields = %v/%d, want none/one", pin.Filename, groupIndex, f.Supers, len(s.Comp.Fields))
				}
				field := s.Comp.Fields[0]
				ref := field.Storage.Val
				if field.Mut != wasm.Const || field.Storage.Packed || ref.Kind != wasm.ValRef || ref.Ref.Nullable || ref.Ref.Exact || ref.Ref.Heap.Kind != wasm.HeapTypeIndex || !ref.Ref.Heap.Type.Rec || ref.Ref.Heap.Type.Index != 0 {
					t.Fatalf("%s root group %d field = %+v, want immutable non-null self reference", pin.Filename, groupIndex, field)
				}
				continue
			}
			if len(f.Supers) != 1 || f.Supers[0].Rec || f.Supers[0].Index != 0 || len(s.Comp.Fields) != 0 {
				t.Fatalf("%s final group super/fields = %v/%d, want flat type 0/empty", pin.Filename, f.Supers, len(s.Comp.Fields))
			}
		}
		gRef := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 4}), false)
		f1Ref := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
		if !m.ReferenceTypeSubtype(gRef, f1Ref) || m.ReferenceTypeSubtype(f1Ref, gRef) {
			t.Fatalf("%s function relation g <: f1 / f1 <: g = %v/%v, want true/false", pin.Filename, m.ReferenceTypeSubtype(gRef, f1Ref), m.ReferenceTypeSubtype(f1Ref, gRef))
		}
	}

	provider, consumer := modules[0], modules[1]
	if len(provider.Imports) != 0 || len(provider.FuncTypes) != 1 || len(provider.Code) != 1 || len(provider.Exports) != 1 || provider.TableCount() != 0 || provider.MemCount() != 0 || len(provider.Globals) != 0 || len(provider.Elements) != 0 || len(provider.Data) != 0 || provider.TagCount() != 0 || provider.Start != nil {
		t.Fatalf("provider state imports/functions/code/exports = %d/%d/%d/%d, want 0/1/1/1 and no other state", len(provider.Imports), len(provider.FuncTypes), len(provider.Code), len(provider.Exports))
	}
	if provider.FuncTypes[0].Rec || provider.FuncTypes[0].Index != 4 || len(provider.Code[0].Locals.Runs) != 0 || !isExactEndBody(provider.Code[0].BodyBytes) {
		t.Fatalf("provider function type/locals/body = %v/%v/%x, want flat type 4/no locals/empty", provider.FuncTypes[0], provider.Code[0].Locals.Runs, provider.Code[0].BodyBytes)
	}
	if provider.Exports[0].Name != "g" || provider.Exports[0].Index.Kind != wasm.ExternFunc || provider.Exports[0].Index.Index != 0 {
		t.Fatalf("provider export = %+v, want g function 0", provider.Exports[0])
	}
	if len(consumer.Imports) != 1 || consumer.ImportedFuncCount() != 1 || len(consumer.FuncTypes) != 0 || len(consumer.Code) != 0 || len(consumer.Exports) != 0 {
		t.Fatalf("consumer imports/functions/code/exports = %d/%d/%d/%d, want 1/0/0/0", len(consumer.Imports), len(consumer.FuncTypes), len(consumer.Code), len(consumer.Exports))
	}
	imp := consumer.Imports[0]
	if imp.Module != "M6" || imp.Name != "g" || imp.Type.Kind != wasm.ExternFunc || imp.Type.Type.Rec || imp.Type.Type.Index != 0 {
		t.Fatalf("consumer import = %+v, want M6.g flat type 0", imp)
	}
	providerTypes, err := typeDescriptorsFromWasm(provider)
	if err != nil {
		t.Fatalf("provider descriptors: %v", err)
	}
	consumerTypes, err := typeDescriptorsFromWasm(consumer)
	if err != nil {
		t.Fatalf("consumer descriptors: %v", err)
	}
	actual := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 4}}
	required := ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}
	if !referenceTypeSubtype(actual, providerTypes, required, consumerTypes) {
		t.Fatal("provider M6.g source type is not a structural subtype of the consumer f1 requirement")
	}
	for _, pin := range pins {
		data := stagedGCTypeSubtypingProductData(t, pin)
		m, _ := wasm.DecodeModule(data)
		product, err := stagedGCTypeSubtypingProductShape(m)
		if err != nil || product != pin.Class {
			t.Fatalf("%s product = %v, %v; want %v", pin.Filename, product, err, pin.Class)
		}
		if !stagedGCTypeSubtypingProductPinned(data, product) {
			t.Fatalf("%s is not in the production pin set", pin.Filename)
		}
	}
}

func TestStagedGCTypeSubtypingProductPlatformAndBoundsGate(t *testing.T) {
	pins := make([]stagedGCTypeSubtypingProductPin, 0, 12)
	for _, pinIndex := range []int{8, 14, 18, 21, 23, 24, 25, 26, 27} {
		pins = append(pins, stagedGCTypeSubtypingProductPins[pinIndex])
	}
	pins = append(pins, stagedGCTypeSubtypingLinkUnlinkablePins...)
	pins = append(pins, stagedGCTypeSubtypingFinalityLinkProviderPin)
	pins = append(pins, stagedGCTypeSubtypingFinalityLinkUnlinkablePins...)
	pins = append(pins, stagedGCTypeSubtypingStructLinkProviderPin, stagedGCTypeSubtypingStructLinkConsumerPin)
	pins = append(pins, stagedGCTypeSubtypingStructProjectionLinkProviderPin, stagedGCTypeSubtypingStructProjectionLinkConsumerPin)
	pins = append(pins, stagedGCTypeSubtypingStructMismatchLinkProviderPin, stagedGCTypeSubtypingStructMismatchLinkConsumerPin)
	pins = append(pins, stagedGCTypeSubtypingIndependentStructLinkProviderPin, stagedGCTypeSubtypingIndependentStructLinkConsumerPin)
	for _, pin := range pins {
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

	finalityCallCast, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingProductPins[24]))
	if err != nil {
		t.Fatal(err)
	}
	finalityCallCast.Types[1].SubTypes[0].Final = false
	if product, err := stagedGCTypeSubtypingProductShape(finalityCallCast); err == nil && product == stagedGCTypeSubtypingRuntimeFinalityCallCast {
		t.Fatal("runtime finality call/cast product with a widened final type unexpectedly retained exact admission")
	}

	structProvider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructLinkProviderPin))
	if err != nil {
		t.Fatal(err)
	}
	structProvider.Types[0].SubTypes[1].Comp.Fields[0].Storage.Val.Ref.Nullable = true
	if product, err := stagedGCTypeSubtypingProductShape(structProvider); err == nil && product == stagedGCTypeSubtypingStructLinkProvider {
		t.Fatal("struct link provider with a nullable recursive field unexpectedly retained exact admission")
	}
	structConsumer, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructLinkConsumerPin))
	if err != nil {
		t.Fatal(err)
	}
	structConsumer.Imports[0].Module = "M4"
	if product, err := stagedGCTypeSubtypingProductShape(structConsumer); err == nil && product == stagedGCTypeSubtypingStructLinkConsumer {
		t.Fatal("struct link consumer with a widened provider namespace unexpectedly retained exact admission")
	}

	projectionProvider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkProviderPin))
	if err != nil {
		t.Fatal(err)
	}
	projectionProvider.Types[2].SubTypes[1].Comp.Fields[1].Storage.Val.Ref.Heap.Type.Index = 0
	if product, err := stagedGCTypeSubtypingProductShape(projectionProvider); err == nil && product == stagedGCTypeSubtypingStructProjectionLinkProvider {
		t.Fatal("struct projection provider with reordered field identity unexpectedly retained exact admission")
	}
	projectionConsumer, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructProjectionLinkConsumerPin))
	if err != nil {
		t.Fatal(err)
	}
	projectionConsumer.Imports[0].Module = "M5"
	if product, err := stagedGCTypeSubtypingProductShape(projectionConsumer); err == nil && product == stagedGCTypeSubtypingStructProjectionLinkConsumer {
		t.Fatal("struct projection consumer with a widened provider namespace unexpectedly retained exact admission")
	}

	mismatchProvider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkProviderPin))
	if err != nil {
		t.Fatal(err)
	}
	mismatchProvider.Types[1].SubTypes[1].Comp.Fields[0].Storage.Val.Ref.Heap.Type.Rec = true
	if product, err := stagedGCTypeSubtypingProductShape(mismatchProvider); err == nil && product == stagedGCTypeSubtypingStructMismatchLinkProvider {
		t.Fatal("struct mismatch provider with a self-recursive second group unexpectedly retained exact admission")
	}
	mismatchConsumer, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingStructMismatchLinkConsumerPin))
	if err != nil {
		t.Fatal(err)
	}
	mismatchConsumer.Imports[0].Module = "M6"
	if product, err := stagedGCTypeSubtypingProductShape(mismatchConsumer); err == nil && product == stagedGCTypeSubtypingStructMismatchLinkConsumer {
		t.Fatal("struct mismatch consumer with a widened provider namespace unexpectedly retained exact admission")
	}

	independentProvider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingIndependentStructLinkProviderPin))
	if err != nil {
		t.Fatal(err)
	}
	independentProvider.Types[1].SubTypes[1].Comp.Fields[0].Storage.Val.Ref.Heap.Type.Rec = false
	if product, err := stagedGCTypeSubtypingProductShape(independentProvider); err == nil && product == stagedGCTypeSubtypingIndependentStructLinkProvider {
		t.Fatal("independent struct provider with an external second-group field unexpectedly retained exact admission")
	}
	independentConsumer, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingIndependentStructLinkConsumerPin))
	if err != nil {
		t.Fatal(err)
	}
	independentConsumer.Imports[0].Module = "M7"
	if product, err := stagedGCTypeSubtypingProductShape(independentConsumer); err == nil && product == stagedGCTypeSubtypingIndependentStructLinkConsumer {
		t.Fatal("independent struct consumer with a widened provider namespace unexpectedly retained exact admission")
	}

	typedTable, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingTypedTablePin))
	if err != nil {
		t.Fatal(err)
	}
	typedTable.Tables[0].Type.Ref = wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
	if product, err := stagedGCTypeSubtypingProductShape(typedTable); err == nil && product == stagedGCTypeSubtypingRuntimeTypedTableCall {
		t.Fatal("runtime typed-table product with widened table storage unexpectedly retained exact admission")
	}

	finalityProvider, err := wasm.DecodeModule(stagedGCTypeSubtypingProductData(t, stagedGCTypeSubtypingFinalityLinkProviderPin))
	if err != nil {
		t.Fatal(err)
	}
	finalityProvider.Types[1].SubTypes[0].Final = false
	if product, err := stagedGCTypeSubtypingProductShape(finalityProvider); err == nil && product == stagedGCTypeSubtypingFinalityLinkProvider {
		t.Fatal("finality link provider with widened final type unexpectedly retained exact admission")
	}
}
