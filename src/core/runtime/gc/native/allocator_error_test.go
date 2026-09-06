package gc

import (
	"fmt"
	"strings"
	"testing"
)

func expectAllocatorInvariantPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), want) {
			t.Fatalf("panic = %v, want substring %q", recovered, want)
		}
	}()
	fn()
}

func TestCollectorAllocatorFreeErrorsFailStopBeforeHandleRelease(t *testing.T) {
	t.Run("throughput", func(t *testing.T) {
		c := newTestCollector(t, Config{})
		parent, err := c.NewArrayDefault(3, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(parent); err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		h := handleOf(parent)
		original := c.handles[h]
		c.handles[h].allocSize = 0
		expectAllocatorInvariantPanic(t, "internal throughput free invariant", func() { c.free(h) })
		if c.handles[h].space != spaceOld || !c.handles[h].remembered || c.handles[h].cardSlot == 0 {
			t.Fatalf("failed free released handle/card state: %+v", c.handles[h])
		}
		c.handles[h] = original
	})

	t.Run("tiny", func(t *testing.T) {
		c := newTinyTestCollector(t, Config{TinyHeapBytes: 4096, TinyBlockBytes: 16})
		object, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		h := handleOf(object)
		original := c.handles[h]
		c.handles[h].off++
		expectAllocatorInvariantPanic(t, "internal tiny free invariant", func() { c.free(h) })
		if c.handles[h].space != spaceTiny || !c.validObjectRef(object) {
			t.Fatalf("failed tiny free released handle: %+v", c.handles[h])
		}
		c.handles[h] = original
	})
}
