//go:build amd64

package gc

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"
)

//go:nocheckptr
func readDirectDirty(c *Collector, r Ref) byte {
	v := c.DirectView()
	if v.Dirty == 0 || uint32(r)/8 >= v.DirtyCount {
		return 0
	}
	value := unsafe.Slice((*byte)(unsafe.Pointer(v.Dirty)), int(v.DirtyCount))[uint32(r)/8]
	runtime.KeepAlive(c)
	return value
}

func TestDirectDirtyViewRegistrationAndRewrite(t *testing.T) {
	array, _ := NewArrayDesc(0, StorageRefNull)
	scalar, _ := NewStructDesc(1, []StorageKind{StorageI32})
	c, err := NewExperimentalDirectCollector(Config{NurseryBytes: 256, ThroughputHeapBytes: 1024}, []TypeDesc{array, scalar})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewArrayDefault(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{parent}
	if err = c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	parent = roots[0]
	v := c.DirectView()
	dirty := v.Dirty
	if readDirectDirty(c, parent) != 0 {
		t.Fatal("new old parent already dirty")
	}
	first, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ArraySet(parent, 0, RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if readDirectDirty(c, parent) != 1 {
		t.Fatal("helper did not publish remembered certificate")
	}
	// Simulate the native fast store: never mutate dirty metadata. Existing
	// registration must cover the entire parent's next reference store as well.
	binary.LittleEndian.PutUint32(c.bytes(parent)[PayloadOffset+4:], uint32(second))
	if err = c.CollectMinor(RefSliceRoots{Ref(2)}); err == nil {
		t.Fatal("bad root accepted")
	}
	if readDirectDirty(c, parent) != 1 {
		t.Fatal("failed collection consumed registration")
	}
	if err = c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	for i, old := range []Ref{first, second} {
		value, e := c.ArrayGet(roots[0], uint32(i))
		if e != nil || value.Ref == old || !c.validObjectRef(value.Ref) {
			t.Fatalf("edge %d not rewritten: %v %v", i, value, e)
		}
	}
	if readDirectDirty(c, roots[0]) != 0 || v.Dirty != dirty {
		t.Fatal("minor reset/backing failure")
	}
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if readDirectDirty(c, roots[0]) != 0 || v.Dirty != dirty {
		t.Fatal("full reset/backing failure")
	}
	c.Close()
	if v.Dirty != 0 || v.DirtyCount != 0 {
		t.Fatal("close exposed dirty backing")
	}
}

func TestDirectDirtyViewRejectsForgedBacking(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI32)
	c, e := NewExperimentalDirectCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	iv, e := BuildDirectInstanceView(c, []TypeID{0})
	if e != nil {
		t.Fatal(e)
	}
	cv := c.DirectView()
	before := *cv
	cv.Dirty++
	if ValidateDirectInstanceView(iv, c) == nil {
		t.Fatal("forged dirty backing accepted")
	}
	*cv = before
	cv.DirtyCount--
	if ValidateDirectInstanceView(iv, c) == nil {
		t.Fatal("forged dirty length accepted")
	}
	*cv = before
	if e = ValidateDirectInstanceView(iv, c); e != nil {
		t.Fatal(e)
	}
}
