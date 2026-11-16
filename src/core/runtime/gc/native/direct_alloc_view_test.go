//go:build amd64

package gc

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"
)

// simulateDirectNativeAllocation follows the native ABI publication sequence.
// Integer pointers are deliberate machine-code ABI operands, owned by c.
//
//go:nocheckptr
func simulateDirectNativeAllocation(t *testing.T, c *Collector, typ uint32, value uint64) Ref {
	t.Helper()
	v := c.DirectView()
	if v.AllocationEnabled != 1 {
		t.Fatal("allocation capability disabled")
	}
	bump := (*uint32)(unsafe.Pointer(v.NurseryBumpAddress))
	if v.AllocationCounter != 0 {
		t.Fatal("ABI4 counter must be reserved zero")
	}
	off := *bump
	if off != v.NurseryBump || off%16 != 0 || off > v.NurseryEnd || v.NurseryEnd-off < 32 {
		t.Fatal("invalid reservation")
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(v.Arena)), int(v.ArenaBytes))
	starts := unsafe.Slice((*byte)(unsafe.Pointer(v.Starts)), int(v.StartCount))
	clear(bytes[off : off+32])
	binary.LittleEndian.PutUint32(bytes[off:], typ)
	binary.LittleEndian.PutUint32(bytes[off+4:], 24)
	binary.LittleEndian.PutUint32(bytes[off+12:], FlagPointerFree)
	binary.LittleEndian.PutUint64(bytes[off+16:], value)
	starts[off/8] = 1
	*bump = off + 32
	v.NurseryBump = off + 32
	runtime.KeepAlive(c)
	return Ref(off)
}

func TestDirectAllocationCapabilityMixedPublication(t *testing.T) {
	if failureInjectionEnabled {
		t.Skip("native allocation disabled in failure-injection builds")
	}
	d, err := NewStructDesc(0, []StorageKind{StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewExperimentalDirectCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	v := c.DirectView()
	r := simulateDirectNativeAllocation(t, c, 0, 777)
	if c.Stats().Allocations != 1 || c.Stats().LiveObjects != 1 {
		t.Fatal("native accounting", c.Stats())
	}
	value, err := c.StructGet(r, 0)
	if err != nil || value.Bits != 777 {
		t.Fatal(value, err)
	}
	roots := RefSliceRoots{r}
	second, err := c.NewStructDefaultWithRoots(0, roots)
	if err != nil {
		t.Fatal(err)
	}
	if second == r || c.Stats().Allocations != 2 {
		t.Fatal("Go cursor/accounting", second, c.Stats())
	}
	roots = append(roots, second)
	if err = c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if v.NurseryBump != v.NurseryStart || c.Stats().LiveObjects != 2 {
		t.Fatal("collection did not consume native publication")
	}
	third := simulateDirectNativeAllocation(t, c, 0, 999)
	roots = append(roots, third)
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{777, 0, 999} {
		value, err = c.StructGet(roots[i], 0)
		if err != nil || value.Bits != want {
			t.Fatalf("root %d: %v %v", i, value, err)
		}
	}
	if c.Stats().Allocations != 3 {
		t.Fatal("double accounting", c.Stats())
	}
	c.Close()
	if v.AllocationEnabled != 0 || v.NurseryBumpAddress != 0 || v.AllocationCounter != 0 || v.NurseryEnd != 0 {
		t.Fatal("closed capability retained", v)
	}
}

func TestDirectAllocationCapabilityPolicy(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI32)
	for _, cfg := range []Config{{NurseryBytes: 128, ThroughputHeapBytes: 1024}, {NurseryBytes: 128, ThroughputHeapBytes: 1024, CollectEveryAlloc: true}, {NurseryBytes: 128, ThroughputHeapBytes: 1024, StressNurseryBytes: 128}} {
		c, err := NewExperimentalDirectCollector(cfg, []TypeDesc{d})
		if err != nil {
			t.Fatal(err)
		}
		v := c.DirectView()
		want := !failureInjectionEnabled && !cfg.CollectEveryAlloc && cfg.StressNurseryBytes == 0
		if (v.AllocationEnabled == 1) != want {
			t.Fatal("policy", cfg, v)
		}
		iv, err := BuildDirectInstanceView(c, []TypeID{0})
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateDirectInstanceView(iv, c); err != nil {
			t.Fatal(err)
		}
		v.AllocationCounter++
		if ValidateDirectInstanceView(iv, c) == nil {
			t.Fatal("forged allocation pointer accepted")
		}
		c.Close()
	}
}

func TestDirectDeferredAccountingEveryAllocAndClose(t *testing.T) {
	d, err := NewStructDesc(0, []StorageKind{StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewExperimentalDirectCollector(Config{NurseryBytes: 128, ThroughputHeapBytes: 1024, CollectEveryAlloc: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 7; i++ {
		if _, err = c.NewStructDefaultWithRoots(0, RefSliceRoots{}); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			if c.Stats().Allocations != i {
				t.Fatal(c.Stats())
			}
		}
	}
	if c.Stats().MinorCollections == 0 {
		t.Fatal("no collection")
	}
	c.Close()
	c.Close()
	if c.Stats().Allocations != 7 || c.Stats().LiveObjects != 0 {
		t.Fatal("close accounting", c.Stats())
	}
}
