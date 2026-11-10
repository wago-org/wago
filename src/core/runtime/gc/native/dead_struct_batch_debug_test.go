//go:build wagodebug

package gc

import (
	"errors"
	"testing"
)

func TestReserveDeadStructAllocationsPreservesInjectedFailure(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	for _, after := range []int{0, 1, 7} {
		c, err := NewCollector(Config{}, []TypeDesc{desc})
		if err != nil {
			t.Fatal(err)
		}
		cleanup := armFailure(c, failHandlePublication, after)
		err = c.ReserveDeadStructAllocations(0, 100, EmptyRoots{})
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
