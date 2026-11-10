//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/hex"
	"testing"
)

// License: ../../tests/fixtures/wasmfyi/LICENSE.
// This is the MIT-licensed wasm.fyi numeric-simpson artifact, SHA-256
// dd688b98faece10fd2805482b576f1af758626d7d70f46227e3ee8d9c254f988.
// Source: https://github.com/JairusSW/wasm.fyi/blob/a7f8d8cc83f6074cd5c418928f0c2f9201114102/corpora/applications/sources/kernels.c
// The manifest uses n=1024 and an exact FNV-1a checksum oracle.
const predicateShiftSimpsonWasm = "0061736d0100000001060160017f017f030201000504010101240608017f01418080040b071602066d656d6f727902000962656e63686d61726b00000ae60101e30101077f41012101419fbab12821020240200041016a4102490d0002400240024020004101470d00410121020c010b200041017121032000417f6a21042000417e7121054101210141012106410021020340200241056a200241026a22076c41016a200641016a20004774200241046a200241016a6c41016a4100410220042002461b7420016a6a2101200641026a21062007210220072005470d000b2003450d01200741016a21020b200241036a20026c41016a4100200241017141016a20022000461b7420016a21010b200141036e41c5bbf288787341938380086c21020b20020b"

func BenchmarkAMD64PredicateShiftSimpson(b *testing.B) {
	module, err := hex.DecodeString(predicateShiftSimpsonWasm)
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig(), module)
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("benchmark")
	if err != nil {
		b.Fatal(err)
	}
	out, err := fn.Invoke(1024)
	if err != nil || len(out) != 1 || out[0] != 1058565808 {
		b.Fatalf("oracle = %v, %v", out, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out, err = fn.Invoke(1024)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(out) != 1 || out[0] != 1058565808 {
		b.Fatalf("oracle = %v", out)
	}
	b.ReportMetric(float64(compiled.CodeSize()), "code-B")
}
