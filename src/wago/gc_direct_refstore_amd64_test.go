//go:build linux && amd64 && (wago_gc_direct_refstore || wago_gc_direct_trusted)

package wago

import (
	"encoding/binary"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func directRefStoreModule() []byte {
	types := wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x63, 0, 1}, []byte{0x60, 0, 0}, []byte{0x60, 1, 0x7f, 1, 0x7f}, []byte{0x60, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x7f})
	init := []byte{0x41, 4, 0xfb, 7, 1, 0x24, 0, 0x41, 0xdb, 0, 0xfb, 0, 0, 0x24, 1, 0x0b}
	store := []byte{0x23, 0, 0x20, 0, 0x23, 1, 0xfb, 14, 1, 0x20, 0, 0x41, 7, 0x6a, 0x0b}
	get := []byte{0x23, 0, 0x20, 0, 0xfb, 11, 1, 0xfb, 2, 0, 0, 0x0b}
	churn := []byte{}
	for i := 0; i < 40; i++ {
		churn = append(churn, 0xfb, 1, 0, 0x1a)
	}
	churn = append(churn, 0x0b)
	child := []byte{0x20, 0, 0xfb, 0, 0, 0x24, 1, 0x0b}
	null := []byte{0xd0, 0, 0x24, 1, 0x0b}
	isnull := []byte{0x23, 0, 0x20, 0, 0xfb, 11, 1, 0xd1, 0x0b}
	hold := []byte{0x23, 1, 0x23, 0, 0x41, 0, 0x23, 1, 0xfb, 14, 1, 0xfb, 2, 0, 0, 0x0b}
	names := []string{"init", "store", "get", "churn", "child", "null", "isnull", "hold"}
	exports := [][]byte{}
	for i, n := range names {
		exports = append(exports, wasmtest.ExportEntry(n, 0, uint32(i)))
	}
	return wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{3}, []byte{2}, []byte{4}, []byte{2}, []byte{3}, []byte{5})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(store), wasmtest.Code(get), wasmtest.Code(churn), wasmtest.Code(child), wasmtest.Code(null), wasmtest.Code(isnull), wasmtest.Code(hold))))
}

func TestDirectGCRefStoreRememberedFastPath(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directRefStoreModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 512, ThroughputHeapBytes: 64 << 10, ForceMajorEveryMinor: true}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	invoke := func(name string, args ...uint64) []uint64 {
		got, e := in.Invoke(name, args...)
		if e != nil {
			t.Fatal(name, e)
		}
		return got
	}
	const sentinel = 0x81234567
	index := in.ctrl[runtime.HostCtrlImportIndexOffset:]
	store := func(i uint64, fast bool) {
		binary.LittleEndian.PutUint32(index, sentinel)
		got := invoke("store", i)
		if len(got) != 1 || got[0] != 7+i {
			t.Fatal(got)
		}
		if native := binary.LittleEndian.Uint32(index) == sentinel; native != fast {
			t.Fatalf("slot%d native%v want%v", i, native, fast)
		}
	}
	invoke("init")
	store(0, true)
	invoke("churn")
	invoke("child", 92)
	store(1, false) // clean old parent: helper must register it before storing
	store(2, true)  // already dirty: certificate covers the entire parent
	invoke("null")
	store(3, true)
	if got := invoke("isnull", 3); len(got) != 1 || got[0] != 1 {
		t.Fatal(got)
	}
	invoke("churn") // major relocation must preserve both helper and native edges
	for i, want := range []uint64{91, 92, 92} {
		got := invoke("get", uint64(i))
		if len(got) != 1 || got[0] != want {
			t.Fatalf("slot%d got%v want%d", i, got, want)
		}
	}
	if in.gc.Stats().FullCollections == 0 {
		t.Fatal("no actual major collection")
	}
}

func TestDirectGCRefStoreRejectsInvalidInputs(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), directRefStoreModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 512, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if _, e = in.Invoke("init"); e != nil {
		t.Fatal(e)
	}
	if _, e = in.Invoke("store", 0); e != nil {
		t.Fatal(e)
	}
	if _, e = in.Invoke("churn"); e != nil {
		t.Fatal(e)
	}
	if got, e := in.Invoke("hold"); e != nil || len(got) != 1 || got[0] != 91 {
		t.Fatal("below-stack root across clean-old fallback", got, e)
	}
	for _, idx := range []uint64{4, 0xffffffff} {
		if _, e = in.Invoke("store", idx); e == nil {
			t.Fatal("accepted bad index", idx)
		}
	}
	original := readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)
	for _, bad := range []uint64{3, original + 2, original + 16, readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)} {
		writeGlobalObject(in.globalCells[1], in.c.Globals[1].Type, bad)
		_, e = in.Invoke("store", 0)
		writeGlobalObject(in.globalCells[1], in.c.Globals[1].Type, original)
		if e == nil {
			t.Fatal("accepted malformed child", bad)
		}
		got, e := in.Invoke("get", 0)
		if e != nil || len(got) != 1 || got[0] != 91 {
			t.Fatal("failed store mutated parent", got, e)
		}
	}
	parent := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, 0)
	_, e = in.Invoke("store", 0)
	writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, parent)
	if e == nil {
		t.Fatal("accepted null parent")
	}
}
