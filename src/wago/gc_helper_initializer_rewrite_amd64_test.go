//go:build linux && amd64 && wago_gc_helper_only

package wago

import (
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// helperInitializerRewriteModule retains reference initializers across allocating
// calls and inside constructors. With a moving collector each child must be read
// from its rewritten authoritative slot, not from a pre-allocation Go copy.
func helperInitializerRewriteModule(kind string) []byte {
	types := wasmtest.Vec(
		[]byte{0x5f, 1, 0x7f, 0},    // 0: struct i32
		[]byte{0x5f, 1, 0x63, 0, 0}, // 1: struct (ref null 0)
		[]byte{0x5e, 0x63, 0, 1},    // 2: mutable array (ref null 0)
		[]byte{0x60, 0, 1, 0x63, 0}, // 3: () -> ref null 0
		[]byte{0x60, 0, 1, 0x7f},    // 4: () -> i32
	)
	// A callee-local survives an extra allocation before returning the child.
	leaf := []byte{1, 1, 0x63, 0, 0x41, 37, 0xfb, 0, 0, 0x21, 0, 0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}
	run := []byte{0x10, 0}
	switch kind {
	case "struct":
		run = append(run, 0xfb, 0, 1, 0xfb, 2, 1, 0, 0xfb, 2, 0, 0)
	case "uniform":
		run = append(run, 0x41, 3, 0xfb, 6, 2, 0x41, 2, 0xfb, 11, 2, 0xfb, 2, 0, 0)
	case "fixed":
		// First initializer is live below an internal call while the second child
		// is allocated; both are then constructor scratch roots during array.new.
		run = append(run, 0x10, 0, 0xfb, 8, 2, 2, 0x22, 0, 0x41, 0, 0xfb, 11, 2, 0xfb, 2, 0, 0, 0x20, 0, 0x41, 1, 0xfb, 11, 2, 0xfb, 2, 0, 0, 0x6a)
	default:
		panic("unknown initializer fixture")
	}
	run = append(run, 0x0b)
	run = append([]byte{1, 1, 0x63, 2}, run...)
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(leaf))), leaf...), append(wasmtest.ULEB(uint32(len(run))), run...))),
	)
}

func TestGCHelperInitializerRoots(t *testing.T) {
	for _, kind := range []string{"struct", "uniform", "fixed"} {
		t.Run(kind, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), helperInitializerRewriteModule(kind))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, cfg := range []GCConfig{{CollectEveryAlloc: true, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}} {
				rt := NewRuntime()
				in, err := Instantiate(c, InstantiateOptions{GC: cfg, store: rt.refStore})
				if err != nil {
					t.Fatal(err)
				}
				want := uint64(37)
				if kind == "fixed" {
					want = 74
				}
				for i := 0; i < 20; i++ {
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("iteration=%d got=%v err=%v", i, got, err)
					}
				}
				in.Close()
				rt.Close()
			}
		})
	}
}
