//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func stagedGCRefEqBytes(t testing.TB) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_eq", &script)
	var latest []byte
	for _, cmd := range script.Commands {
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				t.Fatal(err)
			}
			latest = data
		case "module_instance":
			if _, ok := stagedGCRefEqLeaderPinFor(latest, cmd.Line); ok {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatal("official gc/ref_eq leader not found")
	return nil
}

func compileStagedGCRefEqProduct(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCStructProducts = true
	features.GCArrayProducts = true
	features.GCI31Products = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedGCRefEqProductBoundaryLifecycle(t *testing.T) {
	data := stagedGCRefEqBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil {
		t.Fatal("public Compile admitted staged gc/ref_eq product")
	}
	guardCfg := NewRuntimeConfig()
	guardCfg.boundsChecks = BoundsChecksSignalsBased
	guardFeatures := guardCfg.frontendFeatures()
	guardFeatures.TypedFunctionReferences = true
	guardFeatures.GCStructProducts = true
	guardFeatures.GCArrayProducts = true
	guardFeatures.GCI31Products = true
	if _, err := compileWithFrontendFeatures(guardCfg, data, guardFeatures); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard compile gate = %v", err)
	}

	c, err := compileStagedGCRefEqProduct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.stagedGCStructProduct() != stagedGCStructRefEq || !c.usesGCStructHelpers() || !c.usesGCArrayHelpers() {
		t.Fatalf("product/helper admission = %v/%v/%v", c.stagedGCStructProduct(), c.usesGCStructHelpers(), c.usesGCArrayHelpers())
	}
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 80, TinyBlockBytes: 8, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	state := in.existingGCRefTestTableState()
	if state == nil || state.TableCount != 1 || state.Count != 20 || state.Conversion != nil {
		t.Fatalf("ref.eq table state = %+v", state)
	}
	for iteration := 0; iteration < 100; iteration++ {
		if got, err := in.Invoke("init"); err != nil || len(got) != 0 {
			t.Fatalf("init iteration %d = %v, %v", iteration, got, err)
		}
	}
	desc := state.Descriptor
	if got0, got1 := binary.LittleEndian.Uint64(desc[8:]), binary.LittleEndian.Uint64(desc[16:]); got0 != 0 || got1 != 0 {
		t.Fatalf("null equality slots = %#x/%#x", got0, got1)
	}
	if got2, got3, got4 := binary.LittleEndian.Uint64(desc[8+2*8:]), binary.LittleEndian.Uint64(desc[8+3*8:]), binary.LittleEndian.Uint64(desc[8+4*8:]); got2 != uint64(gc.I31New(7)) || got3 != got2 || got4 != uint64(gc.I31New(8)) {
		t.Fatalf("i31 equality slots = %#x/%#x/%#x", got2, got3, got4)
	}
	for _, index := range []int{5, 6, 7, 8} {
		word := binary.LittleEndian.Uint64(desc[8+index*8:])
		root, err := in.gc.CheckedTableSlot(state.Slots[index])
		if err != nil || word != uint64(root) || !root.IsObj() {
			t.Fatalf("object equality slot %d = %#x root=%#x err=%v", index, word, root, err)
		}
	}
	for _, tc := range []struct {
		i, j uint64
		want uint64
	}{{0, 1, 1}, {2, 3, 1}, {2, 4, 0}, {5, 5, 1}, {5, 6, 0}, {7, 7, 1}, {7, 8, 0}, {5, 7, 0}} {
		got, err := in.Invoke("eq", tc.i, tc.j)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("eq(%d,%d) = %v, %v; want %d", tc.i, tc.j, got, err, tc.want)
		}
	}
	if err := in.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 4 {
		t.Fatalf("ref.eq table live objects = %d, want four distinct object roots", live)
	}

	before := binary.LittleEndian.Uint64(desc[8+5*8:])
	if err := state.set(in.gc, 5, gc.Ref(0x100)); err == nil {
		t.Fatal("forged object ref was accepted by equality table")
	}
	if after := binary.LittleEndian.Uint64(desc[8+5*8:]); after != before {
		t.Fatalf("rejected forged write mutated %#x -> %#x", before, after)
	}
	if err := state.set(in.gc, 20, gc.Null()); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("out-of-bounds equality table write = %v", err)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil {
		t.Fatal("snapshot admitted gc/ref_eq product")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 0 {
		t.Fatalf("closed gc/ref_eq collector retained %d objects", live)
	}

	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.stagedGCStructProduct() != 0 || loaded.usesGCStructHelpers() || loaded.usesGCArrayHelpers() {
		t.Fatal("codec inherited gc/ref_eq admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded gc/ref_eq instantiate = %v", err)
	}

	t.Logf("gc/ref_eq product: wasm=%d code=%d codec=%d tableState=%d plugin=%d", len(data), len(c.Code), len(blob), unsafe.Sizeof(gcRefTestTableState{}), unsafe.Sizeof(instancePluginState{}))
}

func BenchmarkStagedGCRefEqStableIdentity(b *testing.B) {
	c, err := compileStagedGCRefEqProduct(stagedGCRefEqBytes(b))
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
		got, err := in.Invoke("eq", 2, 3)
		if err != nil || len(got) != 1 || got[0] != 1 {
			b.Fatalf("eq(2,3) = %v, %v", got, err)
		}
	}
}
