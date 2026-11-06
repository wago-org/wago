//go:build !tinygo && !wago_target_tinygo && ((linux && (amd64 || arm64)) || (darwin && arm64))

package wago

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestGCParallelNativeRoots(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-parallel-graph.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Workers: workers, NurseryBytes: 1 << 20, ThroughputHeapBytes: 32 << 20, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			const count = 16384
			invoke := func(name string, n, want uint64) {
				t.Helper()
				got, err := in.Invoke(name, n)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s(%d)=%v,%v want=%d", name, n, got, err, want)
				}
			}
			invoke("build", count, count)
			for range 4 {
				invoke("churn", 65536, 65535)
				invoke("sum", count, count*(count-1)/2)
			}
			if in.gc.Stats().MinorCollections == 0 {
				t.Fatal("native fixture did not collect")
			}
		})
	}
}
