//go:build linux && amd64 && !tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// License: ../../tests/fixtures/wasmfyi/LICENSE.
// This is the MIT-licensed wasm.fyi memory64 load/store performance artifact.
// Source: https://github.com/JairusSW/wasm.fyi/blob/a7f8d8cc83f6074cd5c418928f0c2f9201114102/corpora/features/sources/memory64-load-store.wat
// Its performance wrapper repeats the kernel 64 times within Wasm.
const memory64LoadStorePerformanceHex = "0061736d0100000001060160017f017f030302000005050105028002072403066d656d6f727902000962656e63686d61726b00000b706572666f726d616e636500010a5f023b01027f03402002027f200141ff3f71410274ad2001360200200141ff3f71410274ad2802000b6a2102200141016a210120012000490d000b20020b2101027f03402002200010006a2102200141016a2101200141c000490d000b20020b0059046e616d650115010012706572666f726d616e63655f6b65726e656c022602000300016e010169020161010300016e0109697465726174696f6e0208636865636b73756d031302000100046c6f6f7001010006726570656174"

func BenchmarkAMD64Memory64LoadStore(b *testing.B) {
	module, err := hex.DecodeString(memory64LoadStorePerformanceHex)
	if err != nil {
		b.Fatal(err)
	}
	if got := sha256.Sum256(module); hex.EncodeToString(got[:]) != "4d80659d231224bb9e74ed6fdf323d5582cb61bdda921e6d28541d1d14ac6a03" {
		b.Fatalf("artifact hash = %x", got)
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
	compiled, err := Compile(cfg, module)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("performance")
	if err != nil {
		b.Fatal(err)
	}
	out, err := fn.Invoke(4096)
	if err != nil || len(out) != 1 || out[0] != 536739840 {
		b.Fatalf("oracle = %v, %v", out, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out, err = fn.Invoke(4096)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(out) != 1 || out[0] != 536739840 {
		b.Fatalf("oracle = %v", out)
	}
	b.ReportMetric(float64(compiled.CodeSize()), "code-B")
}
