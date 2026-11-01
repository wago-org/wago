package wago

import (
	"encoding/binary"
	"strings"
	"testing"
)

// A rejected publication must preserve the old checked root before any later
// collection or access. This uses bounded ownership metadata only.
func TestGCExternConversionRejectedReplacementPreservesRoot(t *testing.T) {
	_, collector, state, object := newGCExternConversionFixture(t)
	oldWord, err := state.externFromAny(uint64(object))
	if err != nil {
		t.Fatal(err)
	}
	next, err := collector.NewStructDefaultWithRoots(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	newWord, err := state.externFromAny(uint64(next))
	if err != nil {
		t.Fatal(err)
	}
	if err = state.replaceExtern(0, oldWord); err != nil {
		t.Fatal(err)
	}
	var oldSlot uint32
	for i := range state.entries {
		entry := &state.entries[i]
		if entry.externWord == oldWord {
			oldSlot = entry.rootSlot
		}
		if entry.externWord == newWord {
			entry.uses = ^uint32(0)
		}
	}
	descriptor := make([]byte, 16)
	binary.LittleEndian.PutUint32(descriptor, 1)
	binary.LittleEndian.PutUint64(descriptor[8:], oldWord)
	table := &gcRefTestTableState{Descriptors: [][]byte{descriptor}, Conversion: state, TableCount: 1, kinds: []gcRefTableKind{gcRefTableExtern}, entryBytes: []int{8}}
	if err = table.setTable(collector, 0, 0, newWord); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("replacement: %v", err)
	}
	if binary.LittleEndian.Uint64(descriptor[8:]) != oldWord {
		t.Fatal("STOP rejected replacement changed visible slot")
	}
	root, err := collector.CheckedTableSlot(oldSlot)
	if err != nil || root != object {
		t.Fatalf("STOP rejected replacement lost old root: %v", err)
	}
	for i := range state.entries {
		entry := &state.entries[i]
		if entry.externWord == oldWord && entry.uses != 1 {
			t.Fatal("STOP old receipt changed")
		}
		if entry.externWord == newWord {
			if entry.uses != ^uint32(0) {
				t.Fatal("STOP attempted receipt changed")
			}
			entry.uses = 0
		}
	}
	if err = state.close(); err != nil {
		t.Fatal(err)
	}
	collector.Close()
}
