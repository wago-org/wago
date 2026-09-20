//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestExactNativeRootsUseAllocationGCPolicy(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, policy := range []struct {
		name   string
		config GCConfig
	}{
		{"pressure", GCConfig{StressNurseryBytes: 128, VerifyAfterCollect: true}},
		{"stress", GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
		{"disabled", GCConfig{DisableCollection: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, VerifyAfterCollect: true}},
	} {
		for _, mode := range []string{"ordinary", "prepared", "session"} {
			t.Run(policy.name+"/"+mode, func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
				defer rt.Close()
				mod, err := rt.Compile(gcLifecycleModule())
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err := rt.Instantiate(context.Background(), mod, WithGC(policy.config))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if !in.c.GCNativeRootAdmission().Exact {
					t.Fatal("fixture lacks exact native roots")
				}
				const calls = 1000
				t.Run("calls", func(t *testing.T) {
					call := lifecycleCaller(t, in, mode, "set")
					for i := uint64(1); i <= calls; i++ {
						out, err := call(i)
						if err != nil || len(out) != 1 || out[0] != i {
							t.Fatalf("set = %v, %v", out, err)
						}
						ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], ValAnyRef)))
						value, err := in.gc.StructGet(ref, 0)
						if err != nil || uint64(value.I32()) != i {
							t.Fatalf("global after call %d = %v, %v", i, value, err)
						}
					}
				})
				stats := in.gc.Stats()
				switch policy.name {
				case "pressure":
					if stats.MinorCollections == 0 || stats.FullCollections != 0 {
						t.Fatalf("pressure collection = %+v", stats)
					}
				case "stress":
					if stats.MinorCollections+stats.FullCollections != calls {
						t.Fatalf("stress collection = %+v", stats)
					}
				case "tiny", "disabled":
					if stats.FullCollections != calls {
						t.Fatalf("%s full collections = %d, want %d", policy.name, stats.FullCollections, calls)
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if live := in.gc.Stats().LiveObjects; live != 1 {
					t.Fatalf("replaced globals retained %d objects, want 1", live)
				}
			})
		}
	}
}

func TestMissingNativeRootsKeepBoundaryCollection(t *testing.T) {
	_, in := newGCLifecycleInstance(t)
	original := in.c
	copy := *original
	copy.validateMemo = nil
	in.c = &copy
	defer func() { in.c = original }()
	if in.c.genericGCFrameRoots() != nil {
		t.Fatal("fixture still has root maps")
	}
	before := in.gc.Stats().FullCollections
	if err := in.collectGenericGCAtBoundary(); err != nil {
		t.Fatal(err)
	}
	if got := in.gc.Stats().FullCollections; got != before+1 {
		t.Fatalf("missing-root boundary collections = %d, want %d", got, before+1)
	}
}
