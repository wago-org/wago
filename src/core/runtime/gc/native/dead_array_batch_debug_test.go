//go:build wagodebug

package gc

import (
	"errors"
	"testing"
)

func TestReserveDeadArrayAllocationsPreservesInjectedFailure(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, after := range []int{0, 1, 7} {
		c, err := NewCollector(Config{}, []TypeDesc{desc})
		if err != nil {
			t.Fatal(err)
		}
		cleanup := armFailure(c, failHandlePublication, after)
		err = c.ReserveDeadArrayAllocations(0, 16, 100, EmptyRoots{})
		cleanup()
		if !errors.Is(err, errInjectedFailure) {
			t.Fatalf("after=%d error=%v", after, err)
		}
		if got := c.Stats().Allocations; got != uint64(after) {
			t.Fatalf("after=%d allocations=%d", after, got)
		}
		if err := c.Verify(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
}
