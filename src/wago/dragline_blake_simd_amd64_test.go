//go:build amd64 && !tinygo

package wago

import (
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

func TestDraglineBlake3LeafInliningPreservesCallLiveR12(t *testing.T) {
	source, err := os.ReadFile("../../corpus/workloads/semantic/blake3/blake3.wasm")
	if err != nil {
		t.Fatal(err)
	}
	want := "aae792484c8efe4f19e2ca7d371d8c467ffb10748d8a5a1ae579948f718a2a63"
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("bounds=%v/workers=%d", mode, workers), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(mode).WithFunctionWorkers(workers), source)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				result, err := instance.Invoke("blake3_input_ptr")
				if err != nil || len(result) != 1 {
					t.Fatalf("input pointer: %v, %v", result, err)
				}
				input := result[0]
				result, err = instance.Invoke("blake3_output_ptr")
				if err != nil || len(result) != 1 {
					t.Fatalf("output pointer: %v, %v", result, err)
				}
				output := result[0]
				memory := instance.Memory().UnsafeBytes()
				for i := uint64(0); i < 8192; i++ {
					memory[input+i] = byte(i % 251)
				}
				for repeat := 0; repeat < 3; repeat++ {
					if _, err := instance.Invoke("blake3_hash", input, 8192, output); err != nil {
						t.Fatal(err)
					}
					if got := hex.EncodeToString(memory[output : output+32]); got != want {
						t.Fatalf("hash(8192)=%s, want %s", got, want)
					}
				}
			})
		}
	}
}

func TestDraglineBlakeSIMDSpillForwarding(t *testing.T) {
	testDraglineAMD64SpillCorpus(t, "assemblyscript/blake-as-simd.wasm", "_initialize", "hashN", []uint64{100}, 26497025)
}

func TestDraglineMatrixEdgeRematerialization(t *testing.T) {
	testDraglineAMD64SpillCorpus(t, "polybench/3mm.wasm", "", "polybench_run", nil, 2627163156)
}

func testDraglineAMD64SpillCorpus(t *testing.T, artifact, initialize, export string, args []uint64, want uint64) {
	t.Helper()
	source, err := os.ReadFile("../../corpus/workloads/" + artifact)
	if err != nil {
		t.Fatal(err)
	}
	bounds := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		bounds = append(bounds, BoundsChecksSignalsBased)
	}
	for _, mode := range bounds {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("bounds=%v/workers=%d", mode, workers), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(mode).WithFunctionWorkers(workers), source)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				if initialize != "" {
					if _, err := instance.Invoke(initialize); err != nil {
						t.Fatal(err)
					}
				}
				for repeat := 0; repeat < 3; repeat++ {
					got, err := instance.Invoke(export, args...)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("%s(%v) = %v, %v; want [%d]", export, args, got, err, want)
					}
				}
			})
		}
	}
}
