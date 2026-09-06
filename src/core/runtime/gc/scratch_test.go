package gc

import (
	"errors"
	"fmt"
	"testing"
)

type callbackRoots func(func(RootSlot) bool)

func (f callbackRoots) RangeRoots(visit func(RootSlot) bool) { f(visit) }
func scratchCollector(t testing.TB) *Collector {
	t.Helper()
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestScratchReentrantRootsAndGenerationChecks(t *testing.T) {
	for _, keep := range []bool{true, false} {
		t.Run(fmt.Sprint(keep), func(t *testing.T) {
			c := scratchCollector(t)
			ref, err := c.NewStruct(0)
			if err != nil {
				t.Fatal(err)
			}
			root := Root(ref)
			roots := callbackRoots(func(visit func(RootSlot) bool) {
				visit(&root)
				var inner RootSet
				if keep {
					inner = RefSliceRoots{ref}
				}
				if err := c.CollectFull(inner); err != nil {
					t.Fatal(err)
				}
			})
			err = c.CollectFull(roots)
			if keep && err != nil {
				t.Fatal(err)
			}
			if !keep && !errors.Is(err, ErrInvalidReference) {
				t.Fatalf("generation validation after callback: %v", err)
			}
		})
	}
}

func TestScratchPanicCloseAndOutlierRelease(t *testing.T) {
	c := scratchCollector(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("callback panic was hidden")
			}
		}()
		_ = c.CollectFull(callbackRoots(func(visit func(RootSlot) bool) { r := Root(Null()); visit(&r); panic("test callback") }))
	}()
	if c.scratch == nil || len(c.scratch.refs) != 0 {
		t.Fatal("panic retained active roots")
	}
	if cap(c.scratch.refs) > 0 && c.scratch.refs[:cap(c.scratch.refs)][0].owner != nil {
		t.Fatal("scratch retained collector reference")
	}
	roots := make(RefSliceRoots, maxScratchEntries*4)
	s, _, err := c.prepareScratch(roots, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.releaseScratch(s)
	if cap(c.scratch.refs) > maxScratchEntries || cap(c.scratch.native) > maxScratchEntries {
		t.Fatal("outlier storage retained")
	}
	err = c.CollectFull(callbackRoots(func(func(RootSlot) bool) { c.Close() }))
	if !errors.Is(err, ErrCollectorClosed) || c.scratch != nil {
		t.Fatalf("callback close: %v", err)
	}
}

func TestScratchWarmAdapterDoesNotAllocate(t *testing.T) {
	c := scratchCollector(t)
	var roots RootSet = RefSliceRoots{Null(), I31New(1)}
	values := []Value{I32Value(1)}
	if n := testing.AllocsPerRun(100, func() {
		s, _, err := c.prepareScratch(roots, values)
		if err != nil {
			t.Fatal(err)
		}
		c.releaseScratch(s)
	}); n != 0 {
		t.Fatalf("warm adapter allocated %g times", n)
	}
}
