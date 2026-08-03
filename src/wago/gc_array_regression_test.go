//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"encoding/hex"
	"testing"
)

const gcArrayGlobalRootRegressionHex = "0061736d01000000010c035f017f005e7f016000017f03020102060701630001d0000b0707010372756e00000a1801160041cd00fb000024004101fb07011a2300fb0200000b0014046e616d65040702000173010161070401000167"

const gcArrayNonNullNewElemRegressionHex = "0061736d01000000010b035f005e6400016000017f030201020707010372756e000009090105640001fb00000b0a0e010c0041004101fb0a0100fb0f0b0014046e616d65040702000173010161080401000165"

func compileGCArrayRegressionModule(t *testing.T, encoded string) *Compiled {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestGenericGCArrayAllocationSynchronizesMutableGlobalRoots(t *testing.T) {
	compiled := compileGCArrayRegressionModule(t, gcArrayGlobalRootRegressionHex)
	defer compiled.Close()

	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("run")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != 77 {
				t.Fatalf("run = %v, want [77]", got)
			}
		})
	}
}

func TestGenericGCArrayNewElemSupportsNonNullReferenceElements(t *testing.T) {
	compiled := compileGCArrayRegressionModule(t, gcArrayNonNullNewElemRegressionHex)
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("run = %v, want [1]", got)
	}
}
