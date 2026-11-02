package wago

import (
	"encoding/binary"
	"fmt"
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

func TestGCExternConversionPublicationOrderingAndRollback(t *testing.T) {
	_, collector, state, first := newGCExternConversionFixture(t)
	defer collector.Close()
	defer state.close()
	oldWord, err := state.externFromAny(uint64(first))
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.NewStructDefaultWithRoots(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	newWord, err := state.externFromAny(uint64(second))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.replaceExtern(0, oldWord); err != nil {
		t.Fatal(err)
	}
	visible := oldWord
	oldEntry, err := state.dataEntryForExternLocked(oldWord)
	if err != nil {
		t.Fatal(err)
	}
	newEntry, err := state.dataEntryForExternLocked(newWord)
	if err != nil {
		t.Fatal(err)
	}
	oldSlot := oldEntry.rootSlot
	failure := fmt.Errorf("publication rejected before write")
	for _, fail := range []bool{true, false} {
		err := state.publishExternReplacement(oldWord, newWord, func() error {
			if oldEntry.uses != 1 || newEntry.uses != 1 || visible != oldWord {
				t.Fatal("STOP wrong receipt counts before visible write")
			}
			root, e := collector.CheckedTableSlot(oldSlot)
			if e != nil || root != first {
				t.Fatal("STOP old root released before publication")
			}
			if fail {
				return failure
			}
			visible = newWord
			return nil
		})
		if fail {
			if err != failure || oldEntry.uses != 1 || newEntry.uses != 0 || visible != oldWord {
				t.Fatal("STOP failed publication changed ownership/storage")
			}
			continue
		}
		if err != nil || visible != newWord || oldEntry.kind != 0 || newEntry.uses != 1 {
			t.Fatal("replacement failed")
		}
		root, e := collector.CheckedTableSlot(oldSlot)
		if e != nil || !root.IsNull() {
			t.Fatal("old root survived committed replacement")
		}
	}
	if err := state.publishExternReplacement(newWord, newWord, func() error { return nil }); err != nil || newEntry.uses != 1 {
		t.Fatal("same-value replacement changed receipt balance")
	}
	if err := state.publishExternReplacement(newWord, 0, func() error { visible = 0; return nil }); err != nil || newEntry.kind != 0 || visible != 0 {
		t.Fatal("clear failed")
	}
}
