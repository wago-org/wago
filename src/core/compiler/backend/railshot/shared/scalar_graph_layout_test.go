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
	if now.Name() != "ScalarState" || now.PkgPath() != old.PkgPath() {
		t.Fatalf("ordinary type identity changed: %s.%s", now.PkgPath(), now.Name())
	}
	if old.Size() != now.Size() || old.Align() != now.Align() || old.NumField() != now.NumField() {
		t.Fatalf("ordinary layout: before=%d/%d/%d after=%d/%d/%d", old.Size(), old.Align(), old.NumField(), now.Size(), now.Align(), now.NumField())
	}
	for i := 0; i < old.NumField(); i++ {
		a := old.Field(i)
		b := now.Field(i)
		if a.Name != b.Name || a.Offset != b.Offset || a.Type != b.Type || a.Anonymous != b.Anonymous || a.Tag != b.Tag || a.PkgPath != b.PkgPath {
			t.Fatalf("ordinary field layout changed: %s", a.Name)
		}
	}
}
