//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCArrayBulkLeaderBytes(t testing.TB, base string) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
	for _, cmd := range script.Commands {
		if cmd.Type != "module_definition" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("%s has no module definition", base)
	return nil
}

func TestStagedGCArrayBulkProductBoundary(t *testing.T) {
	for _, tc := range []struct {
		base        string
		product     stagedGCArrayProduct
		tinyBytes   uint32
		invoke      string
		args        []uint64
		getIndex    uint64
		wantElement uint64
	}{
		{base: "gc/array_fill", product: stagedGCArrayProductBulkFill, tinyBytes: 64, invoke: "array_fill", args: []uint64{2, 0x10b, 2}, getIndex: 2, wantElement: 11},
		{base: "gc/array_copy", product: stagedGCArrayProductBulkCopy, tinyBytes: 64, invoke: "array_copy", args: []uint64{0, 0, 2}, getIndex: 0, wantElement: 10},
	} {
		t.Run(tc.base, func(t *testing.T) {
			data := stagedGCArrayBulkLeaderBytes(t, tc.base)
			guardCfg := NewRuntimeConfig()
			guardCfg.boundsChecks = BoundsChecksSignalsBased
			features := guardCfg.frontendFeatures()
			features.TypedFunctionReferences = true
			features.GCArrayProducts = true
			if _, err := compileWithFrontendFeatures(guardCfg, data, features); err == nil || !strings.Contains(err.Error(), "signals-based") {
				t.Fatalf("guard compile=%v, want explicit bulk-array rejection", err)
			}

			c, err := compileStagedGCArrayBulk(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := c.stagedGCArrayProduct(); got != tc.product {
				t.Fatalf("product=%s, want %s", got, tc.product)
			}
			if !c.usesGCArrayHelpers() {
				t.Fatal("bulk array product omitted helper admission")
			}
			if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC") {
				t.Fatalf("snapshot capture=%v, want WasmGC rejection", err)
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
			if loaded.stagedGCArrayProduct() != 0 || loaded.usesGCArrayHelpers() {
				t.Fatal("codec reload inherited bulk array product/helper admission")
			}
			if in, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil {
				_ = in.Close()
				t.Fatal("codec-loaded bulk array artifact instantiated")
			}
			t.Logf("%s product: wasm=%d code=%d codec=%d", tc.base, len(data), len(c.Code), len(blob))

			in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: tc.tinyBytes, TinyBlockBytes: 16, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if state := in.pluginState.Load(); state == nil || state.gcGlobalRootCount != 2 {
				t.Fatalf("bulk array global roots=%#v, want two", state)
			}
			if _, err := in.Invoke(tc.invoke, tc.args...); err != nil {
				t.Fatalf("%s=%v", tc.invoke, err)
			}
			got, err := in.Invoke("array_get_nth", tc.getIndex)
			if err != nil || len(got) != 1 || got[0] != tc.wantElement {
				t.Fatalf("array_get_nth=%v,%v want %d", got, err, tc.wantElement)
			}
		})
	}
	t.Logf("bulk array layouts: Compiled=%d Instance=%d codeCache=%d memoryDir=%d arrayGlobal=%d plugin=%d collector=%d", unsafe.Sizeof(Compiled{}), unsafe.Sizeof(Instance{}), unsafe.Sizeof(compiledCodeCache{}), unsafe.Sizeof(compiledMemoryDirectory{}), unsafe.Sizeof(gcArrayGlobalInit{}), unsafe.Sizeof(instancePluginState{}), unsafe.Sizeof(corergc.Collector{}))
}

func TestStagedGCArrayBulkCopyMutableRootLifecycle(t *testing.T) {
	data := stagedGCArrayBulkLeaderBytes(t, "gc/array_copy")
	c, err := compileStagedGCArrayBulk(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 96, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	state := in.pluginState.Load()
	if state == nil || state.gcGlobalRootCount != 2 {
		t.Fatalf("root state=%#v, want two mappings", state)
	}
	mutable := state.gcGlobalRoots[1]
	before := readGlobalObject(in.globalCells[mutable.GlobalIndex], ValAnyRef)
	if _, err := in.Invoke("array_copy_overlap_test-1"); err != nil {
		t.Fatalf("first overlap copy: %v", err)
	}
	firstAfter := readGlobalObject(in.globalCells[mutable.GlobalIndex], ValAnyRef)
	if firstAfter == before || firstAfter>>32 != 0 {
		t.Fatalf("first mutable compact global %#x -> %#x", before, firstAfter)
	}
	for i := 1; i < 100; i++ {
		field := "array_copy_overlap_test-1"
		if i&1 != 0 {
			field = "array_copy_overlap_test-2"
		}
		if _, err := in.Invoke(field); err != nil {
			t.Fatalf("iteration %d %s: %v", i, field, err)
		}
	}
	after := readGlobalObject(in.globalCells[mutable.GlobalIndex], ValAnyRef)
	if after>>32 != 0 {
		t.Fatalf("mutable compact global has high bits: %#x", after)
	}
	rooted, err := in.gc.CheckedGlobalSlot(mutable.SlotIndex)
	if err != nil || uint64(rooted) != after {
		t.Fatalf("mutable root=%v,%v want %#x", rooted, err, after)
	}
	if _, err := in.Invoke("array_copy", 0, 0, 13); err == nil {
		t.Fatal("out-of-bounds copy returned normally")
	}
	if got := readGlobalObject(in.globalCells[mutable.GlobalIndex], ValAnyRef); got != after {
		t.Fatalf("trapping copy changed mutable global %#x -> %#x", after, got)
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 2 {
		t.Fatalf("live objects=%d, want immutable plus current mutable array", live)
	}
}

func BenchmarkStagedGCArrayBulkFill(b *testing.B) {
	data := stagedGCArrayBulkLeaderBytes(b, "gc/array_fill")
	c, err := compileStagedGCArrayBulk(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("array_fill", 2, 11, 2); err != nil {
			b.Fatal(err)
		}
	}
}
