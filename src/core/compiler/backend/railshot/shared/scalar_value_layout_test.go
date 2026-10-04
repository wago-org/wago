package shared

import (
	"testing"
	"unsafe"
)

func TestScalarValueBoundsAndLayout(t *testing.T) {
	if scalarMaxReferences >= 1<<16 || scalarMaxNodes >= 1<<16 {
		t.Fatal("admission no longer fits compact counters")
	}
	type previous struct {
		constant          int64
		left, right       scalarID
		refs, slot        int32
		op                IntOp
		kind              ScalarLocation
		reg, depth        uint8
		wide, operandWide bool
		home              uint16
	}
	if unsafe.Sizeof(scalarNode{}) != unsafe.Sizeof(previous{}) {
		t.Fatalf("node size grew from %d to %d", unsafe.Sizeof(previous{}), unsafe.Sizeof(scalarNode{}))
	}
	var state ScalarState
	state.add(scalarNode{})
	for i := 1; i <= scalarMaxNodes; i++ {
		id := state.add(scalarNode{refs: 1})
		if state.node(id).order != uint16(i) {
			t.Fatalf("id=%d order=%d", id, state.node(id).order)
		}
	}
	if scalarValueChecks {
		t.Run("creation-limit", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing creation bound check")
				}
			}()
			state.add(scalarNode{refs: 1})
		})
		t.Run("dead-reference", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing reference check")
				}
			}()
			state.node(1).refs = 0
			state.retain(1)
		})
	}
}
