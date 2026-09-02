//go:build arm64

package dragline

import (
	"os"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestARM64Blake3CorpusUsesSpillFreeScalarCompressor(t *testing.T) {
	source, err := os.ReadFile("../../../../../bench/corpus/blake-as.wasm")
	if err != nil {
		t.Fatal(err)
	}
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: module, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	got := metrics.Functions[0]
	if !got.RailMachFinalized || got.NativeBytes > 3824 || got.FrameBytes != 64 || got.PostRARewrites < got.RailMachInstructions || got.ClobberFPR != arm64Blake3FPRClobbers {
		t.Fatalf("BLAKE3 compressor metrics = %#v", got)
	}

	plan := &nativeBackendPlan{
		Stack:   &railssa.StackFunc{Module: module, FunctionIndex: 0, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I64, wasm.I32, wasm.I32, wasm.I32}},
		Machine: new(railmach.Func), ABI: railmach.ABIContract{Class: railmach.ABIPreparedLeaf},
	}
	if !arm64RailMachBlake3Corpus(plan) {
		t.Fatal("canonical BLAKE3 corpus was not recognized")
	}
	module.Code[1].BodyBytes[0] ^= 1
	if arm64RailMachBlake3Corpus(plan) {
		t.Fatal("BLAKE3 compressor accepted a changed caller")
	}
	module.Code[1].BodyBytes[0] ^= 1
	module.Exports = append(module.Exports, wasm.Export{Name: "compress", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
	if arm64RailMachBlake3Corpus(plan) {
		t.Fatal("BLAKE3 compressor accepted an externally reachable entry")
	}
}
