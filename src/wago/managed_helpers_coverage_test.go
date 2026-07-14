package wago

import (
	"context"
	"reflect"
	"testing"
)

func TestManagedCapabilityGuardHelpers(t *testing.T) {
	if voidFuncTypeID() == 0 {
		t.Fatal("void function type ID is zero")
	}
	m := newPendingInstanceManager("plugin", CapabilityBudget{MaxInstances: 2})
	if m.owner != "plugin" || m.budget.MaxInstances != 2 || len(m.instances) != 0 || len(m.byInstance) != 0 {
		t.Fatalf("pending manager = %#v", m)
	}
	if _, err := m.Caller(nil); err == nil {
		t.Fatal("Caller accepted nil host module")
	}
	if _, err := m.ManagedCaller(nil); err == nil {
		t.Fatal("ManagedCaller accepted nil host module")
	}
	if ch, cancel, err := m.WatchCaller(nil); err == nil || ch != nil || cancel != nil {
		t.Fatalf("WatchCaller(nil) = channel %v, cancel present %v, error %v", ch, cancel != nil, err)
	}
	if _, err := (&InstanceManager{closed: true}).ensureVoidDispatcher(); err == nil {
		t.Fatal("closed manager created void dispatcher")
	}
	closed := &ManagedInstance{}
	if err := closed.ValidateVoidTableEntry(0); err == nil {
		t.Fatal("closed managed instance accepted table entry")
	}
	if err := closed.InvokeVoidTable(context.Background(), 0); err == nil {
		t.Fatal("closed managed instance invoked table entry")
	}
	noTable := &ManagedInstance{value: &Instance{c: &Compiled{}}}
	if err := noTable.ValidateVoidTableEntry(0); err == nil {
		t.Fatal("tableless managed instance accepted table entry")
	}
}

func TestImportDedupPreservesInsertionOrderAndReleasesReferences(t *testing.T) {
	var d importDedup[int]
	for _, v := range []int{3, 1, 4, 1, 5, 9} {
		d.add(v)
	}
	if !d.contains(4) || d.contains(2) || d.n != 5 {
		t.Fatalf("dedup membership = %#v", d)
	}
	var got []int
	d.each(func(v int) { got = append(got, v) })
	if want := []int{3, 1, 4, 5, 9}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dedup order = %v, want %v", got, want)
	}
	d.reset()
	if d.n != 0 || d.extra != nil || d.contains(3) || d.inline[0] != 0 {
		t.Fatalf("dedup reset = %#v", d)
	}
}
