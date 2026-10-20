//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"unsafe"
)

func gcProofArtifact(t *testing.T, c *Compiled, payload []byte) []byte {
	t.Helper()
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		t.Fatal(err)
	}
	var root compiledWriter
	root.gcFrameRoots(c.genericGCFrameRoots())
	if !bytes.HasSuffix(metadata, root.buf) {
		t.Fatal("root section must be final metadata payload")
	}
	metadata = append(metadata[:len(metadata)-len(root.buf)], payload...)
	var w compiledWriter
	w.buf = append(w.buf, wagoMagic...)
	w.u8(wagoVersion)
	w.u8(compiledHeader)
	w.section(compiledSectionCode, c.code)
	w.section(compiledSectionMetadata, metadata)
	return w.buf
}

func TestGCScalarAdapterCodecProofValidation(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcScalarAdapterProofModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root := c.genericGCFrameRoots()
	var w compiledWriter
	w.gcFrameRoots(root)
	if got := c.GCNativeRootAdmission().MetadataBytes; got != uint64(len(w.buf)) {
		t.Fatalf("root wire size = %d, admission reported %d", len(w.buf), got)
	}
	prefix := compiledUvarintLen(gcFrameRootsFormatMarker) + compiledUvarintLen(gcFrameRootsFormatVersion)
	legacy := compiledWriter{}
	legacy.uvar(uint64(len(root.adapterReturnOffsets)))
	for _, off := range root.adapterReturnOffsets {
		legacy.u32(off)
	}
	legacy.uvar(uint64(len(root.safepoints)))
	for _, point := range root.safepoints {
		legacy.u32(point.id)
		legacy.u32(point.frameBytes)
		legacy.uvar(uint64(len(point.offsets)))
		for _, off := range point.offsets {
			legacy.u32(off)
		}
	}
	legacy.uvar(uint64(len(root.callsites)))
	for _, site := range root.callsites {
		legacy.u32(site.returnOffset)
		legacy.u32(site.frameBytes)
		legacy.u32(site.stackAdjust)
		legacy.uvar(uint64(len(site.offsets)))
		for _, off := range site.offsets {
			legacy.u32(off)
		}
	}
	unknown := append([]byte(nil), w.buf...)
	unknown[prefix-1]++
	for _, tc := range []struct {
		name    string
		payload []byte
		want    string
	}{
		{"legacy", legacy.buf, "recompile"},
		{"missing-version", w.buf[:prefix-1], "recompile"},
		{"unknown-version", unknown, "recompile"},
		{"truncated-record", w.buf[:len(w.buf)-1], ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loaded Compiled
			defer loaded.Close()
			err := loaded.UnmarshalBinary(gcProofArtifact(t, c, tc.payload))
			if err == nil || (tc.want != "" && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("artifact error = %v, want rejection containing %q", err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*compiledGCFrameRoots)
	}{
		{"invalid-index", func(r *compiledGCFrameRoots) { r.safepoints[0].functionProof = uint32(len(c.Funcs))<<1 | 1 }},
		{"wrong-scalar-bit", func(r *compiledGCFrameRoots) { r.safepoints[0].functionProof ^= 1 }},
		{"collector-scalar-forgery", func(r *compiledGCFrameRoots) { r.safepoints[1].functionProof |= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := *root
			mutated.safepoints = append([]compiledGCFrameSafepoint(nil), root.safepoints...)
			tc.change(&mutated)
			var payload compiledWriter
			payload.gcFrameRoots(&mutated)
			var loaded Compiled
			defer loaded.Close()
			if err := loaded.UnmarshalBinary(gcProofArtifact(t, c, payload.buf)); err == nil || !strings.Contains(err.Error(), "function-signature proof") {
				t.Fatalf("forged artifact error = %v", err)
			}
		})
	}
	t.Logf("sizes: safepoint=%d callsite=%d frame=%d activation=%d public=%d; wire=%d prefix=%d records=%d", unsafe.Sizeof(compiledGCFrameSafepoint{}), unsafe.Sizeof(compiledGCFrameCallsite{}), unsafe.Sizeof(gcNativeFrameRoots{}), unsafe.Sizeof(gcHostActivation{}), unsafe.Sizeof(gcPublicState{}), len(w.buf), prefix, len(root.safepoints)+len(root.callsites))
}

func TestGCScalarAdapterCodecRoundTrip(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", registerABI)
		for _, module := range [][]byte{gcScalarAdapterProofModule(), gcScalarAdapterCallerModule("unused-table", true, true)} {
			c, err := Compile(cfg, module)
			if err != nil {
				t.Fatal(err)
			}
			loaded := publicArtifactRoundTrip(t, c)
			original, restored := c.genericGCFrameRoots(), loaded.genericGCFrameRoots()
			if original != nil {
				if restored == nil || len(original.safepoints) != len(restored.safepoints) || len(original.callsites) != len(restored.callsites) {
					t.Fatal("root records lost through public artifact")
				}
				for i := range original.safepoints {
					if original.safepoints[i].functionProof != restored.safepoints[i].functionProof {
						t.Fatal("safepoint function proof changed")
					}
				}
				for i := range original.callsites {
					if original.callsites[i].functionProof != restored.callsites[i].functionProof {
						t.Fatal("callsite function proof changed")
					}
				}
			}
			loaded.Close()
			c.Close()
		}
	}
}

func TestGCScalarAdapterCodecCallsiteProof(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcScalarAdapterCallerModule("unused-table", true, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root := c.genericGCFrameRoots()
	if root == nil || len(root.callsites) == 0 {
		t.Fatal("missing callsite control")
	}
	for _, proof := range []uint32{uint32(len(c.Funcs))<<1 | 1, root.callsites[0].functionProof ^ 1} {
		changed := *root
		changed.callsites = append([]compiledGCFrameCallsite(nil), root.callsites...)
		changed.callsites[0].functionProof = proof
		var payload compiledWriter
		payload.gcFrameRoots(&changed)
		var loaded Compiled
		err := loaded.UnmarshalBinary(gcProofArtifact(t, c, payload.buf))
		loaded.Close()
		if err == nil || !strings.Contains(err.Error(), "function-signature proof") {
			t.Fatalf("callsite proof %#x admitted: %v", proof, err)
		}
	}
}

func TestGCScalarAdapterCodecExecution(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", registerABI)
		rt := NewRuntime(WithRuntimeConfig(cfg))
		settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
		producerCode, err := rt.Compile(gcAllocatingHostScalarProducerModule())
		if err != nil {
			t.Fatal(err)
		}
		producerLoaded := publicArtifactRoundTrip(t, producerCode.c)
		producerModule, err := rt.Module(producerLoaded)
		if err != nil {
			t.Fatal(err)
		}
		defer producerModule.Close()
		producer, err := rt.Instantiate(context.Background(), producerModule, WithGC(settings), WithImports(testImports("env.host", slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) { results[0] = 7 }))))
		if err != nil {
			t.Fatal(err)
		}
		target, err := producer.ExportedFunc("call")
		if err != nil {
			t.Fatal(err)
		}
		callerCode, err := rt.Compile(gcScalarAdapterCallerModule("indirect", true, true))
		if err != nil {
			t.Fatal(err)
		}
		callerLoaded := publicArtifactRoundTrip(t, callerCode.c)
		callerModule, err := rt.Module(callerLoaded)
		if err != nil {
			t.Fatal(err)
		}
		defer callerModule.Close()
		caller, err := rt.Instantiate(context.Background(), callerModule, WithGC(settings), WithImports(testImports("env.call", target)))
		if err != nil {
			t.Fatal(err)
		}
		if caller.gc == nil || caller.gc == producer.gc {
			t.Fatal("fixture requires separate collectors")
		}
		for i := 0; i < 3; i++ {
			got, err := caller.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 73 {
				t.Fatalf("reloaded scalar call = %v, %v; want [73], nil", got, err)
			}
		}
		caller.Close()
		producer.Close()
		callerLoaded.Close()
		callerCode.Close()
		producerLoaded.Close()
		producerCode.Close()
		rt.Close()
	}
}
