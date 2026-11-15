//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// This is an optimization witness, not a baseline correctness defect.
func TestOwnedHandleGrowthPreservesBase(t *testing.T) {
	c := newTestCollector(t, Config{DisableCollection: true, NurseryBytes: 4096, ThroughputHeapBytes: 16 << 20})
	c.UseOwnedThroughputBacking()
	first, err := c.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(first, 0, I32Value(42)); err != nil {
		t.Fatal(err)
	}
	base := unsafe.SliceData(c.handles)
	for range 20000 {
		if _, err = c.NewStruct(0); err != nil {
			t.Fatal(err)
		}
	}
	if unsafe.SliceData(c.handles) != base {
		t.Fatal("owned handle growth copied the table to a new address")
	}
	got, err := c.StructGet(first, 0)
	if err != nil || got.I32() != 42 {
		t.Fatalf("growth changed first object: %v,%v", got, err)
	}
	if c.NativeView().HandleCount != uint32(len(c.handles)) {
		t.Fatal("native view published capacity instead of logical count")
	}
}

func TestOwnedNativeHandleGrowthPreservesBase(t *testing.T) {
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{NurseryBytes: 2 << 20, ThroughputHeapBytes: 16 << 20}, []TypeDesc{desc})
	c.UseOwnedThroughputBacking()
	size, err := StructSize(desc)
	if err != nil {
		t.Fatal(err)
	}
	if !c.PrepareNativeStructHandles() {
		t.Fatal("first native reservation was rejected")
	}
	base := unsafe.SliceData(c.handles)
	for batch := 0; batch < 8; batch++ {
		if batch != 0 && !c.PrepareNativeStructHandles() {
			t.Fatal("native refill rejected")
		}
		s := &c.nativeStructAlloc
		for s.Cursor < s.Count {
			h := s.Handles[s.Cursor]
			c.handles[h] = handleEntry{off: c.nurseryBump, size: size, space: spaceNursery}
			c.writeHeader(makeObjRef(h), ObjHeader{TypeID: 0, Size: size, Flags: FlagPointerFree})
			c.nurseryBump += size
			s.Cursor++
		}
	}
	c.CancelNativeAllocationBatch()
	if unsafe.SliceData(c.handles) != base {
		t.Fatal("native reservation growth copied the handle table")
	}
	if got := c.Stats().Allocations; got != uint64(8*nativeStructHandleBatch) {
		t.Fatalf("allocation charge=%d", got)
	}
	if err = c.Verify(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedHandleBackingReleaseAndBound(t *testing.T) {
	c := newTestCollector(t, Config{DisableCollection: true, NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20})
	c.UseOwnedThroughputBacking()
	if _, err := c.NewStruct(0); err != nil {
		t.Fatal(err)
	}
	if c.handlesBacking != handlesBackingMapped {
		t.Fatal("owned handle map not active")
	}
	full := c.mappedHandleBacking()
	before := unsafe.SliceData(c.handles)
	count := len(c.handles)
	if c.growOwnedHandles(ownedHandleEntries+1) || unsafe.SliceData(c.handles) != before || len(c.handles) != count || c.handlesBacking != handlesBackingMapped {
		t.Fatal("unrepresentable extent changed live owner")
	}
	c.Close()
	c.Close()
	if c.handlesBacking != handlesBackingDefault || len(c.handles) != 0 || c.NativeView().Handles != 0 {
		t.Fatal("Close retained handle owner")
	}
	if err := syscall.Munmap(full); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("original full mapping was not released: %v", err)
	}
}

func TestOwnedHandleBackingReservationFailure(t *testing.T) {
	const marker = "WAGO_TEST_HANDLE_RESERVATION_FAILURE"
	if os.Getenv(marker) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedHandleBackingReservationFailure$", "-test.v")
		cmd.Env = append(os.Environ(), marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fallback subprocess: %v\n%s", err, out)
		}
		return
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := strconv.ParseUint(strings.Fields(string(data))[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	var old syscall.Rlimit
	if err = syscall.Getrlimit(syscall.RLIMIT_AS, &old); err != nil {
		t.Fatal(err)
	}
	limit := old
	limit.Cur = pages*uint64(os.Getpagesize()) + (256 << 20)
	if limit.Cur > limit.Max {
		t.Fatal("insufficient existing hard address-space limit")
	}
	if err = syscall.Setrlimit(syscall.RLIMIT_AS, &limit); err != nil {
		t.Fatal(err)
	}
	c := newTestCollector(t, Config{DisableCollection: true, NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20})
	c.UseOwnedThroughputBacking()
	first, err := c.NewStruct(0)
	if err != nil {
		t.Fatal(err)
	}
	if c.handlesBacking != handlesBackingFailed {
		t.Fatal("failed full map did not select Go fallback")
	}
	if err = syscall.Setrlimit(syscall.RLIMIT_AS, &old); err != nil {
		t.Fatal(err)
	}
	for range 5000 {
		if _, err = c.NewStruct(0); err != nil {
			t.Fatal(err)
		}
	}
	if c.handlesBacking != handlesBackingFailed {
		t.Fatal("failed map was retried after the limit was restored")
	}
	if _, err = c.StructGet(first, 0); err != nil {
		t.Fatal(err)
	}
}
