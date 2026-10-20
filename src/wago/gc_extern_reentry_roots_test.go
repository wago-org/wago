//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"testing"
)

func TestGCSharedRuntimeStackABIReentryRoots(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", false)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	c, err := Compile(cfg, gcHostReentryFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	m, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var in *Instance
	host := slotHostFunc(func(caller HostModule, _, results []uint64) {
		got, err := in.InvokeFromHost(context.Background(), caller, "inner")
		if err != nil || len(got) != 1 || got[0] != 0 {
			panic(HostTrap{Err: err})
		}
		results[0] = 0
	})
	in, err = rt.Instantiate(context.Background(), m, WithImports(testImports("env.reenter", host)), WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.refStore.ownsGCCollector(in.gc) {
		t.Fatal("fixture does not have shared Runtime collector")
	}
	got, err := in.Invoke("outer", 91)
	if err != nil || len(got) != 1 || got[0] != 91 {
		t.Fatalf("outer:%v,%v; want91", got, err)
	}
}
