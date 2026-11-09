//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_target_tinygo

package wago

import (
	"sync/atomic"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func managedStorageClassificationModule(field byte, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, field, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body), wasmtest.Code([]byte{0xd0, 0x6f, 0x24, 0, 0x0b}))),
	)
}

// The execution snapshot caches its managed-storage classification once. Each
// collecting aggregate dispatch then reads that state, and only the snapshot
// that owns the validation memo trusts it.
func TestManagedStorageClassificationCachedForExecutionSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field byte
		body  []byte
		want  bool
	}{
		{"function-field", 0x70, []byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}, true},
		{"numeric-field", 0x7f, []byte{0x41, 1, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), managedStorageClassificationModule(tc.field, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			exec := in.c
			memo := exec.loadValidateMemo()
			if exec == c || memo == nil || memo.executionView() != exec {
				t.Fatal("instance does not run on the frozen execution snapshot")
			}
			for i := 0; i < 64; i++ {
				if _, err := in.Invoke("run"); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			if _, err := in.Invoke("clear"); err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("run"); err != nil {
				t.Fatal(err)
			}
			want := uint32(1)
			if tc.want {
				want = 2
			}
			if got := compiledManagedFunctionReachability(exec); got != tc.want {
				t.Fatalf("execution classification %t, want %t", got, tc.want)
			}
			if got := atomic.LoadUint32(&memo.executionManagedStorage); got != want {
				t.Fatalf("cached state %d, want %d", got, want)
			}
			if got := compiledManagedFunctionReachability(c); got != tc.want {
				t.Fatalf("public view classification %t, want %t", got, tc.want)
			}
			if got := scanManagedFunctionReachability(exec); got != tc.want {
				t.Fatalf("fresh scan %t disagrees with cache %t", got, tc.want)
			}
		})
	}
}

// A copy shares the memo but not the snapshot identity, so it never inherits
// the cached answer for metadata it does not own.
func TestManagedStorageClassificationCopyRescans(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), managedStorageClassificationModule(0x70, []byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	exec := c.executionView()
	if exec == c || !compiledManagedFunctionReachability(exec) {
		t.Fatal("missing managed execution snapshot")
	}
	copied := *exec
	copied.GCTypeDescs = nil
	if compiledManagedFunctionReachability(&copied) {
		t.Fatal("copied view reused the snapshot classification")
	}
	if !compiledManagedFunctionReachability(exec) {
		t.Fatal("snapshot classification changed after copy query")
	}
}
