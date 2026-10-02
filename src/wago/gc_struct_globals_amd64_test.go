//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCStructNumericGlobalsBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCStructNumericGlobalsHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCStructGlobalRootsAndPublicEgress(t *testing.T) {
	data := stagedGCStructNumericGlobalsBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public compile unexpectedly admitted GC constant-expression globals")
	}

	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 32, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCStruct(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := len(c.memoryDir.gcStructGlobals); got != 2 {
				t.Fatalf("compiled GC globals = %d, want 2", got)
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			collector := in.gc
			if collector == nil {
				t.Fatal("GC global product has nil collector")
			}
			state := in.pluginState.Load()
			if state == nil || state.gcGlobalRootCount != 2 {
				t.Fatalf("GC global root mapping = %#v", state)
			}
			for i := uint8(0); i < state.gcGlobalRootCount; i++ {
				mapping := state.gcGlobalRoots[i]
				want := gc.Ref(uint32(readGlobalObject(in.globalCells[mapping.GlobalIndex], ValAnyRef)))
				got, err := collector.CheckedGlobalSlot(mapping.SlotIndex)
				if err != nil || got != want || !got.IsObj() {
					t.Fatalf("mapping %d = global %d slot %d ref %v, %v; want live %v", i, mapping.GlobalIndex, mapping.SlotIndex, got, err, want)
				}
			}
			if err := collector.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatalf("collect rooted globals: %v", err)
			}
			if stats := collector.Stats(); stats.LiveObjects != 2 {
				t.Fatalf("collector stats after rooted collection = %+v, want 2 live objects", stats)
			}
			for _, name := range []string{"g0", "g1"} {
				if got, err := in.Global(name); got != 0 || err == nil || !strings.Contains(err.Error(), "reference type") {
					t.Fatalf("Global(%q) = %d, %v; want opaque reference rejection", name, got, err)
				}
				if _, err := in.GlobalValue(name); err == nil || !strings.Contains(err.Error(), "public GC/exception reference egress is unsupported") {
					t.Fatalf("GlobalValue(%q) = %v, want explicit non-null GC egress rejection", name, err)
				}
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := collector.CheckedGlobalSlot(0); err == nil || !strings.Contains(err.Error(), "collector closed") {
				t.Fatalf("root access after close = %v, want collector closed", err)
			}
		})
	}
}

func TestStagedGCStructGlobalRootRollbackAndCodecGate(t *testing.T) {
	data := stagedGCStructNumericGlobalsBytes(t)
	c, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		if in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 16}}); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
			if in != nil {
				_ = in.Close()
			}
			t.Fatalf("tiny rooted-global instantiation %d = %v, want deterministic second-allocation failure", i, err)
		}
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.memoryDir != nil && len(loaded.memoryDir.gcStructGlobals) != 0 {
		t.Fatal("codec reload inherited live GC global initializer sidecar")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded GC-global instantiate = %v, want required-feature rejection", err)
	}
}
