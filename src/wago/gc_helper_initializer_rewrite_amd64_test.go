//go:build linux && amd64 && wago_gc_helper_only

package wago

import (
	"testing"
)

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
