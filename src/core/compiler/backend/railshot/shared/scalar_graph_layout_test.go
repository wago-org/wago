//go:build !wago_regalloccheck

package shared

//lint:file-ignore U1000 layout mirror fields are inspected through reflection

import (
	"reflect"
	"testing"
)

// The pre-integration production layout. Comparing each offset catches padding
// shifts as well as added retained state; a size-only assertion is insufficient.
type scalarStateBeforeGraph struct {
	nodes                       []scalarNode
	stack, locals               []scalarID
	widths                      []bool
	controls                    []scalarControl
	freeSlots                   []int
	owners                      [64]scalarID
	nextSlot, tempBase, maxSlot int
	regs                        []uint8
	reserved                    uint64
	target                      ScalarTarget
	Peak, Discarded             uint64
	Spills, Reloads             int
}

func TestScalarGraphOrdinaryLayout(t *testing.T) {
	old, now := reflect.TypeOf(scalarStateBeforeGraph{}), reflect.TypeOf(ScalarState{})
	if old.Size() != now.Size() || old.Align() != now.Align() {
		t.Fatalf("ordinary layout: before=%d/%d after=%d/%d", old.Size(), old.Align(), now.Size(), now.Align())
	}
	state, _ := now.FieldByName("scalarGraphState")
	if state.Type.Size() != 0 || state.Offset != 0 {
		t.Fatal("nonempty ordinary graph state")
	}
	for i := 0; i < old.NumField(); i++ {
		a := old.Field(i)
		b, ok := now.FieldByName(a.Name)
		if !ok || a.Offset != b.Offset || a.Type != b.Type {
			t.Fatalf("ordinary field layout changed: %s", a.Name)
		}
	}
}
