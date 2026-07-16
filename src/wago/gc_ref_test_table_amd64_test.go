//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const stagedGCRefTestTableHex = "0061736d01000000019b808080000550005f005001005f017f005001005f017e0060000060017f017f0385808080000403040404048480808000016b0002079e808080000404696e6974000004626173650001046c656674000205726967687400030ac080808000049080808000004100fb010126004101fb010226000b89808080000020002500fb14000b89808080000020002500fb14010b89808080000020002500fb14020b"

func stagedGCRefTestTableBytes(t testing.TB) []byte {
	t.Helper()
	data, err := hex.DecodeString(stagedGCRefTestTableHex)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStagedGCRefTestTableProfiles(t *testing.T) {
	data := stagedGCRefTestTableBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public Compile admitted staged object ref.test product")
	}
	for _, tc := range []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 72, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCStruct(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.stagedGCStructProduct() != stagedGCStructRefTestTable || !c.usesGCStructHelpers() || c.stagedFeatures()&CoreFeatureGC == 0 {
				t.Fatalf("product/helpers/features = %v/%v/%v", c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.stagedFeatures())
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			state := in.existingGCRefTestTableState()
			if in.gc == nil || state == nil || state.Count != 2 {
				t.Fatalf("collector/table state = %v/%+v", in.gc, state)
			}
			for slot := uint64(0); slot < 2; slot++ {
				if got := invokeRefTestTable(t, in, "base", slot); got != 0 {
					t.Fatalf("base before init slot %d = %d, want 0", slot, got)
				}
			}
			for iteration := 0; iteration < 100; iteration++ {
				if got, err := in.Invoke("init"); err != nil || len(got) != 0 {
					t.Fatalf("init iteration %d = %v, %v", iteration, got, err)
				}
				for _, call := range []struct {
					name string
					slot uint64
					want uint64
				}{
					{name: "base", slot: 0, want: 1}, {name: "base", slot: 1, want: 1},
					{name: "left", slot: 0, want: 1}, {name: "left", slot: 1, want: 0},
					{name: "right", slot: 0, want: 0}, {name: "right", slot: 1, want: 1},
				} {
					if got := invokeRefTestTable(t, in, call.name, call.slot); got != call.want {
						t.Fatalf("iteration %d %s(%d) = %d, want %d", iteration, call.name, call.slot, got, call.want)
					}
				}
			}
			for i, want := range []gc.TypeID{1, 2} {
				rooted, err := in.gc.CheckedTableSlot(state.Slots[i])
				if err != nil {
					t.Fatal(err)
				}
				actual, err := in.gc.ObjectType(rooted)
				if err != nil || actual != want {
					t.Fatalf("slot %d rooted type = %d, %v; want %d", i, actual, err, want)
				}
				if bits := binary.LittleEndian.Uint64(state.Descriptor[8+i*8:]); bits != uint64(rooted) {
					t.Fatalf("slot %d descriptor = %#x, root = %#x", i, bits, rooted)
				}
			}
			if err := in.gc.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if stats := in.gc.Stats(); stats.LiveObjects != 2 {
				t.Fatalf("collector live objects after full collection = %d, want two rooted table values", stats.LiveObjects)
			}
		})
	}
}

func invokeRefTestTable(t testing.TB, in *Instance, name string, slot uint64) uint64 {
	t.Helper()
	got, err := in.Invoke(name, slot)
	if err != nil || len(got) != 1 {
		t.Fatalf("%s(%d) = %v, %v", name, slot, got, err)
	}
	return got[0]
}

func TestStagedGCRefTestTableTrapAtomicityAndTinyFailure(t *testing.T) {
	c, err := compileStagedGCStruct(stagedGCRefTestTableBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state := in.existingGCRefTestTableState()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	beforeRoot, err := in.gc.CheckedTableSlot(state.Slots[0])
	if err != nil {
		t.Fatal(err)
	}
	beforeBits := binary.LittleEndian.Uint64(state.Descriptor[8:16])
	if err := state.set(in.gc, 0, gc.Ref(0xfffe)); err == nil {
		t.Fatal("table root accepted forged compact ref")
	}
	afterRoot, err := in.gc.CheckedTableSlot(state.Slots[0])
	if err != nil {
		t.Fatal(err)
	}
	if afterRoot != beforeRoot || binary.LittleEndian.Uint64(state.Descriptor[8:16]) != beforeBits {
		t.Fatal("rejected table root write mutated root or native descriptor")
	}
	if err := state.set(in.gc, 2, gc.Null()); err == nil {
		t.Fatal("out-of-bounds table root write succeeded")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := in.gc.RefTest(beforeRoot, gc.RefTestTarget{Kind: gc.RefTestAny}); !strings.Contains(err.Error(), "collector closed") {
		t.Fatalf("ref.test after close = %v, want collector closed", err)
	}

	tiny, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer tiny.Close()
	if _, err := tiny.Invoke("init"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("Tiny init error = %v, want bounded exhaustion", err)
	}
	if got := invokeRefTestTable(t, tiny, "left", 0); got != 1 {
		t.Fatalf("Tiny committed first table.set = %d, want 1", got)
	}
	if got := invokeRefTestTable(t, tiny, "base", 1); got != 0 {
		t.Fatalf("Tiny untouched second slot = %d, want 0", got)
	}
}

func TestStagedGCRefTestTableProductClosure(t *testing.T) {
	data := stagedGCRefTestTableBytes(t)
	c, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.stagedFeatures().IsEnabled(CoreFeatureGC) {
		t.Fatalf("codec inherited object ref.test admission: product=%v helpers=%v features=%v", loaded.stagedGCStructProduct(), loaded.usesGCStructHelpers(), loaded.stagedFeatures())
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded instantiate = %v, want required-feature rejection", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || (!strings.Contains(err.Error(), "tables") && !strings.Contains(err.Error(), "GC")) {
		t.Fatalf("Capture object ref.test product = %v, want table/GC rejection", err)
	}

	cfg := NewRuntimeConfig()
	cfg.boundsChecks = BoundsChecksSignalsBased
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	if _, err := compileWithFrontendFeatures(cfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard compile = %v, want explicit rejection", err)
	}
	unknown := append([]byte(nil), data...)
	for i := 0; i+5 <= len(unknown); i++ {
		if string(unknown[i:i+5]) == "right" {
			unknown[i+4] = 'x'
			break
		}
	}
	cfg = NewRuntimeConfig()
	features = cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	if _, err := compileWithFrontendFeatures(cfg, unknown, features); err == nil {
		t.Fatal("unsupported widened object ref.test shape unexpectedly compiled")
	}

	t.Logf("object ref.test product: wasm=%d code=%d codec=%d state=%d plugin=%d", len(data), len(c.Code), len(blob), unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCRefTestTable(b *testing.B) {
	c, err := compileStagedGCStruct(stagedGCRefTestTableBytes(b))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("base", 0)
		if err != nil || len(got) != 1 || got[0] != 1 {
			b.Fatalf("base = %v, %v", got, err)
		}
	}
}
