//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func stagedGCArrayInitLeaderBytes(t testing.TB, filename string) []byte {
	t.Helper()
	base := "gc/array_init_data"
	if strings.HasPrefix(filename, "array_init_elem.") {
		base = "gc/array_init_elem"
	}
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, base, &script)
	for _, cmd := range script.Commands {
		if cmd.Type != "module_definition" || cmd.Filename != filename {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("%s has no module definition", filename)
	return nil
}

func TestGenericGCArrayInitElemExecutesWithoutFixtureHash(t *testing.T) {
	data, err := os.ReadFile("testdata/gc_array_init_elem_generic.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		t.Fatalf("compile generic array.init_elem: %v", err)
	}
	defer c.Close()
	if got := c.stagedGCArrayProduct(); got != stagedGCArrayProductGeneric {
		t.Fatalf("array product = %s, want generic", got)
	}
	if len(c.GCTypeDescs) < 2 || c.GCTypeDescs[1].Kind != corergc.KindArray || c.GCTypeDescs[1].Elem != corergc.StorageFuncRefNull || len(c.passiveElems) != 2 {
		t.Fatalf("generic type/segment metadata = %+v/%d", c.GCTypeDescs, len(c.passiveElems))
	}
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("generic array.init_elem result = %v, %v; want [1]", got, err)
	}
}

func TestStagedGCArrayInitDataTinyLifecycle(t *testing.T) {
	data := stagedGCArrayInitLeaderBytes(t, "array_init_data.2.wasm")
	c, err := compileStagedGCArrayInit(data)
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
	if state == nil || state.gcGlobalRootCount != 3 {
		t.Fatalf("root state=%#v, want three global roots", state)
	}
	for i := 0; i < 100; i++ {
		if _, err := in.Invoke("array_init_data", 4, 2, 2); err != nil {
			t.Fatalf("iteration %d i8 init: %v", i, err)
		}
		if _, err := in.Invoke("array_init_data_i16", 2, 5, 2); err != nil {
			t.Fatalf("iteration %d i16 init: %v", i, err)
		}
	}
	before, err := in.Invoke("array_get_nth_i16", 2)
	if err != nil || len(before) != 1 || before[0] != 0x6766 {
		t.Fatalf("i16 before trap=%v,%v", before, err)
	}
	if _, err := in.Invoke("array_init_data_i16", 2, 11, 1); err == nil {
		t.Fatal("short i16 source returned normally")
	}
	after, err := in.Invoke("array_get_nth_i16", 2)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatalf("trapping init changed destination: before=%v after=%v err=%v", before, after, err)
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 3 {
		t.Fatalf("live objects=%d, want three rooted arrays", live)
	}
	if _, err := in.Invoke("drop_segs"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("array_init_data", 0, 0, 0); err != nil {
		t.Fatalf("zero length after drop: %v", err)
	}
	if _, err := in.Invoke("array_init_data", 0, 0, 1); err == nil {
		t.Fatal("non-zero init after drop returned normally")
	}
}

func TestStagedGCArrayInitDataTransientTinyRootProof(t *testing.T) {
	data := stagedGCArrayInitLeaderBytes(t, "array_init_data.3.wasm")
	c, err := compileStagedGCArrayInit(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 100; i++ {
		field := "f4"
		if i&1 != 0 {
			field = "g8"
		}
		if _, err := in.Invoke(field); err != nil {
			t.Fatalf("iteration %d %s: %v", i, field, err)
		}
	}
	if _, err := in.Invoke("f3"); err == nil {
		t.Fatal("short i32 source returned normally")
	}
	if _, err := in.Invoke("g8"); err != nil {
		t.Fatalf("recovery after trap: %v", err)
	}
}

func BenchmarkStagedGCArrayInitElem(b *testing.B) {
	data := stagedGCArrayInitLeaderBytes(b, "array_init_elem.3.wasm")
	c, err := compileStagedGCArrayInit(data)
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
		if _, err := in.Invoke("array_init_elem", 2, 3, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCArrayInitData(b *testing.B) {
	data := stagedGCArrayInitLeaderBytes(b, "array_init_data.2.wasm")
	c, err := compileStagedGCArrayInit(data)
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
		if _, err := in.Invoke("array_init_data", 4, 2, 2); err != nil {
			b.Fatal(err)
		}
	}
}
