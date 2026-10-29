//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"
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
