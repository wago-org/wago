//go:build amd64

package gc

import (
	"testing"
	"unsafe"
)

func TestDirectViewLayoutAndRefresh(t *testing.T) {
	if e := validateDirectViewLayout(); e != nil {
		t.Fatal(e)
	}
	d, e := NewArrayDesc(0, StorageI32)
	if e != nil {
		t.Fatal(e)
	}
	c, e := NewExperimentalDirectCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	v := c.DirectView()
	if v == nil || v.Version != DirectABIVersion || v.Size != DirectCollectorViewSize || v.Arena == 0 || v.Starts == 0 || v.NurseryStart != v.NurseryBump || v.OldStart != v.OldBump {
		t.Fatal("initial view", v)
	}
	arena, starts := v.Arena, v.Starts
	r, e := c.NewArrayDefault(0, 1)
	if e != nil {
		t.Fatal(e)
	}
	if c.DirectView() != v || v.NurseryBump <= v.NurseryStart {
		t.Fatal("allocation not published")
	}
	// The immutable byte table has the direct offset/8 index, not a handle index.
	if v.StartCount <= uint32(r)/8 {
		t.Fatal("missing start byte")
	}
	roots := RefSliceRoots{r}
	if e = c.CollectMinor(roots); e != nil {
		t.Fatal(e)
	}
	if roots[0] == r || v.NurseryBump != v.NurseryStart || v.OldBump <= v.OldStart {
		t.Fatal("minor not published", v, roots)
	}
	oldStart := v.OldStart
	if e = c.CollectFull(roots); e != nil {
		t.Fatal(e)
	}
	if v.OldStart == oldStart || v.Arena != arena || v.Starts != starts {
		t.Fatal("semispace/backing view", v)
	}
	before := *v
	if e = c.CollectFull(RefSliceRoots{Ref(2)}); e == nil {
		t.Fatal("invalid roots accepted")
	}
	if *v != before {
		t.Fatal("failed collection changed descriptor")
	}
	c.Close()
	if v.Arena != 0 || v.Starts != 0 || v.ArenaBytes != 0 || v.StartCount != 0 || c.DirectView() != nil {
		t.Fatal("close left pointers", v)
	}
}
func TestDirectInstanceViewOwnsCanonicalMap(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI32)
	c, e := NewExperimentalDirectCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	local := []TypeID{0}
	v, e := BuildDirectInstanceView(c, local)
	if e != nil {
		t.Fatal(e)
	}
	if v.Version != DirectABIVersion || v.Size != 32 || v.Collector != uintptr(unsafe.Pointer(c.DirectView())) || v.LocalTypeCount != 1 || v.LocalTypes == 0 {
		t.Fatal("instance view", v)
	}
	if e = ValidateDirectInstanceView(v, c); e != nil {
		t.Fatal(e)
	}
	v.Size = 0
	if e = ValidateDirectInstanceView(v, c); e == nil {
		t.Fatal("malformed ABI accepted")
	}
	v.Size = DirectInstanceViewABISize
	local[0] = 100
	if v.keepTypes[0] != 0 {
		t.Fatal("mutable map aliased")
	}
	if _, e = BuildDirectInstanceView(c, local); e == nil {
		t.Fatal("invalid canonical map accepted")
	}
	if _, e = BuildDirectInstanceView(nil, nil); e == nil {
		t.Fatal("nil collector accepted")
	}
}
