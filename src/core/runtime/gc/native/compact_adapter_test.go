//go:build amd64 && !tinygo && !wago_target_tinygo && wago_gc_compact_helpers

package gc

import "testing"

func TestCompactAdapterMovesInitializerAndPersistentRoots(t *testing.T) {
	leaf, _ := NewStructDesc(0, []StorageKind{StorageI32})
	pair, _ := NewStructDesc(1, []StorageKind{StorageRefNull, StorageI64})
	arr, _ := NewArrayDesc(2, StorageRefNull)
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 4096, CollectEveryAlloc: true}, []TypeDesc{leaf, pair, arr})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if c.NativeView() != nil || c.PrepareNativeStructAllocation(24) {
		t.Fatal("direct collector advertised handle ABI")
	}
	r, e := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: 777}}, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	global, e := c.NewCheckedGlobalSlot(r)
	if e != nil {
		t.Fatal(e)
	}
	table, e := c.NewCheckedTableSlot(r)
	if e != nil {
		t.Fatal(e)
	}
	before := r
	values := []Value{{Kind: StorageRefNull, Ref: r}, {Kind: StorageI64, Bits: 99}}
	var scratch InitializerRootScratch
	parent, e := c.NewStructWithRootScratch(1, values, EmptyRoots{}, &scratch)
	if e != nil {
		t.Fatal(e)
	}
	if values[0].Ref == before || c.GlobalSlot(global) != values[0].Ref || c.TableSlot(table) != values[0].Ref {
		t.Fatal("initializer/persistent roots not relocated")
	}
	root := Root(parent)
	if e = c.CollectFull(Slots{&root, &root}); e != nil {
		t.Fatal(e)
	}
	v, e := c.StructGet(Ref(root), 0)
	if e != nil {
		t.Fatal(e)
	}
	child, e := c.StructGet(v.Ref, 0)
	if e != nil || child.Bits != 777 {
		t.Fatalf("child %+v %v", child, e)
	}
	var arrayScratch ArrayInitializerRootScratch
	ar, e := c.NewArrayWithRootScratch(2, 4, Value{Kind: StorageRefNull, Ref: v.Ref}, Slots{&root}, &arrayScratch)
	if e != nil {
		t.Fatal(e)
	}
	arRoot := Root(ar)
	if e = c.CollectFull(Slots{&root, &arRoot}); e != nil {
		t.Fatal(e)
	}
	for i := uint32(0); i < 4; i++ {
		v, e = c.ArrayGet(Ref(arRoot), i)
		if e != nil {
			t.Fatal(e)
		}
		child, e = c.StructGet(v.Ref, 0)
		if e != nil || child.Bits != 777 {
			t.Fatal(child, e)
		}
	}
	if c.Stats().LiveObjects != 3 || c.Stats().FullCollections < 2 {
		t.Fatal(c.Stats())
	}
}

func TestCompactAdapterOldToYoungBarrier(t *testing.T) {
	leaf, _ := NewStructDesc(0, []StorageKind{StorageI32})
	box, _ := NewStructDesc(1, []StorageKind{StorageRefNull})
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 4096}, []TypeDesc{leaf, box})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	p, e := c.NewStructDefaultWithRoots(1, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	root := Root(p)
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	v, e := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: 42}}, Slots{&root})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.StructSet(Ref(root), 0, Value{Kind: StorageRefNull, Ref: v}); e != nil {
		t.Fatal(e)
	}
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	field, e := c.StructGet(Ref(root), 0)
	if e != nil {
		t.Fatal(e)
	}
	value, e := c.StructGet(field.Ref, 0)
	if e != nil || value.Bits != 42 || field.Ref == v {
		t.Fatal(value, e)
	}
}

func TestCompactAdapterOldOOMReclaimsAndInitializersSurvive(t *testing.T) {
	leaf, _ := NewStructDesc(0, []StorageKind{StorageI32})
	arr, _ := NewArrayDesc(1, StorageRefNull)
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 64, ThroughputHeapBytes: 256}, []TypeDesc{leaf, arr})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	// A dead old object consumes almost all old space. Minor collection succeeds
	// but cannot reclaim it; allocation must then run a full collection and retry.
	if _, e = c.NewArrayDefaultWithRoots(1, 40, EmptyRoots{}); e != nil {
		t.Fatal(e)
	}
	child, e := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: 987}}, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	var scratch ArrayInitializerRootScratch
	a, e := c.NewArrayWithRootScratch(1, 20, Value{Kind: StorageRefNull, Ref: child}, EmptyRoots{}, &scratch)
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats().FullCollections == 0 {
		t.Fatal("old-only OOM did not trigger full collection")
	}
	root := Root(a)
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	for i := uint32(0); i < 20; i++ {
		v, e := c.ArrayGet(Ref(root), i)
		if e != nil {
			t.Fatal(e)
		}
		v, e = c.StructGet(v.Ref, 0)
		if e != nil || v.Bits != 987 {
			t.Fatal(v, e)
		}
	}
}

func TestCompactAdapterTypeAppendAndForcedMajor(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024, ForceMajorEveryMinor: true}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	bad, _ := NewArrayDesc(9, StorageI8)
	if c.AddTypes([]TypeDesc{bad}) == nil {
		t.Fatal("bad canonical ID accepted")
	}
	good, _ := NewArrayDesc(1, StorageI8)
	if e = c.AddTypes([]TypeDesc{good}); e != nil {
		t.Fatal(e)
	}
	r, e := c.NewArrayDefaultWithRoots(1, 3, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	root := Root(r)
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	n, e := c.ArrayLen(Ref(root))
	if e != nil || n != 3 || c.Stats().FullCollections != 1 {
		t.Fatal(n, e, c.Stats())
	}
}

func TestCompactAdapterNumericPayloadGeometry(t *testing.T) {
	d, _ := NewStructDesc(0, []StorageKind{StorageI32})
	d.Final = true
	a, _ := NewArrayDesc(1, StorageI16)
	a.Final = true
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 256, ThroughputHeapBytes: 4096}, []TypeDesc{d, a})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	r, e := c.NewStructWithRoots(0, []Value{I32Value(31)}, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	h, e := c.direct.Header(directRef(r))
	if e != nil || h.PayloadOffset != 4 || h.Extent != 8 || h.Stride != 16 {
		t.Fatal(h, e)
	}
	root := Root(r)
	ar, e := c.NewArrayFixedWithRoots(1, []Value{{Kind: StorageI16, Bits: 1}, {Kind: StorageI16, Bits: 2}, {Kind: StorageI16, Bits: 3}}, Slots{&root})
	if e != nil {
		t.Fatal(e)
	}
	arRoot := Root(ar)
	v, e := c.ArrayPayload(ar)
	if e != nil || len(v.Bytes) != 6 || cap(v.Bytes) != 6 {
		t.Fatal(v, e)
	}
	v.Bytes[2] = 77
	if e = c.ArrayCopy(ar, 1, ar, 0, 2); e != nil {
		t.Fatal(e)
	}
	if e = c.ArrayInitData(ar, 0, []byte{8, 0}, 0, 1); e != nil {
		t.Fatal(e)
	}
	if e = c.CollectFull(Slots{&root, &arRoot}); e != nil {
		t.Fatal(e)
	}
	for i, w := range []uint64{8, 1, 77} {
		v, e := c.ArrayGet(Ref(arRoot), uint32(i))
		if e != nil || v.Bits != w {
			t.Fatal(i, v, e)
		}
	}
	if v, _, ok, e := c.StructGetTyped(Ref(root), 0, true, 0); e != nil || !ok || v.Bits != 31 {
		t.Fatal(v, ok, e)
	}
	if c.DirectView() != nil {
		t.Fatal("compact exposed direct ABI")
	}
	if _, e := BuildDirectInstanceView(c, []TypeID{0}); e == nil {
		t.Fatal("compact native ABI admitted")
	}
}

func TestCompactAdapterReferenceBulkAndSIMD(t *testing.T) {
	leaf, _ := NewStructDesc(0, []StorageKind{StorageI32})
	arr, _ := NewArrayDesc(1, StorageRefNull)
	vec, _ := NewArrayDesc(2, StorageV128)
	c, e := NewExperimentalCompactCollector(Config{NurseryBytes: 256, ThroughputHeapBytes: 4096}, []TypeDesc{leaf, arr, vec})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	ar, e := c.NewArrayDefaultWithRoots(1, 4, EmptyRoots{})
	if e != nil {
		t.Fatal(e)
	}
	root := Root(ar)
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	child, e := c.NewStructWithRoots(0, []Value{I32Value(55)}, Slots{&root})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.ArrayFill(Ref(root), 0, RefValue(child), 2); e != nil {
		t.Fatal(e)
	}
	if e = c.ArrayCopy(Ref(root), 2, Ref(root), 0, 2); e != nil {
		t.Fatal(e)
	}
	if e = c.CollectMinor(Slots{&root}); e != nil {
		t.Fatal(e)
	}
	for i := uint32(0); i < 4; i++ {
		v, e := c.ArrayGet(Ref(root), i)
		if e != nil {
			t.Fatal(e)
		}
		x, e := c.StructGet(v.Ref, 0)
		if e != nil || x.Bits != 55 {
			t.Fatal(x, e)
		}
	}
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i + 1)
	}
	vr, e := c.NewArrayDataWithRoots(2, 2, data, 0, Slots{&root})
	if e != nil {
		t.Fatal(e)
	}
	vh, e := c.direct.Header(directRef(vr))
	if e != nil || vh.PayloadOffset != 16 {
		t.Fatal(vh, e)
	}
	vrroot := Root(vr)
	if e = c.CollectFull(Slots{&root, &vrroot}); e != nil {
		t.Fatal(e)
	}
	view, e := c.ArrayPayload(Ref(vrroot))
	if e != nil || len(view.Bytes) != 32 || cap(view.Bytes) != 32 {
		t.Fatal(view, e)
	}
	for i, x := range view.Bytes {
		if x != data[i] {
			t.Fatal(i, x)
		}
	}
	if _, _, e := c.checkedHeaderPayload(Null()); e == nil {
		t.Fatal("null header accepted")
	}
}
