package shared

import (
	"errors"
	"testing"
	"unsafe"
)

func TestResolveWorkers(t *testing.T) {
	for _, tc := range []struct {
		requested, functions, gomaxprocs int
		want                             int
	}{
		{0, 8, 8, 1},
		{1, 8, 8, 1},
		{8, 1, 8, 1},
		{8, 3, 8, 3},
		{8, 8, 3, 3},
		{4, 8, 8, 4},
		{4, 8, 0, 1},
	} {
		if got := ResolveWorkers(tc.requested, tc.functions, tc.gomaxprocs); got != tc.want {
			t.Errorf("ResolveWorkers(%d, %d, %d) = %d, want %d", tc.requested, tc.functions, tc.gomaxprocs, got, tc.want)
		}
	}
}

func TestPressureThreshold(t *testing.T) {
	if got := PressureThreshold(123, 800); got != 123 {
		t.Fatalf("explicit threshold = %d", got)
	}
	if got := PressureThreshold(0, 800); got != 700 {
		t.Fatalf("default threshold = %d, want 700", got)
	}
}

var moduleEntriesSink []int

func TestModuleEntriesUsesOneExactBackingAllocation(t *testing.T) {
	entry, internal := ModuleEntries(8)
	if len(entry) != 8 || cap(entry) != 8 || len(internal) != 8 || cap(internal) != 8 {
		t.Fatalf("entry table shapes = %d/%d and %d/%d, want 8/8 and 8/8", len(entry), cap(entry), len(internal), cap(internal))
	}
	if got, want := uintptr(unsafe.Pointer(&internal[0])), uintptr(unsafe.Pointer(&entry[0]))+uintptr(len(entry))*unsafe.Sizeof(entry[0]); got != want {
		t.Fatalf("internal entry table starts at %#x, want adjacent address %#x", got, want)
	}
	entry[0] = 11
	internal[0] = 22
	if entry[0] != 11 || internal[0] != 22 {
		t.Fatalf("entry tables alias values: entry=%d internal=%d", entry[0], internal[0])
	}
	allocs := testing.AllocsPerRun(100, func() {
		moduleEntriesSink, _ = ModuleEntries(128)
	})
	if allocs > 1 {
		t.Fatalf("ModuleEntries allocations = %.0f, want one exact backing allocation", allocs)
	}

	emptyEntry, emptyInternal := ModuleEntries(0)
	if emptyEntry == nil || emptyInternal == nil {
		t.Fatal("zero-function entry tables changed from non-nil empty slices")
	}
}

func TestFirstErrorIndex(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	errs := []error{nil, first, second}
	idx, err := FirstErrorIndex(len(errs), func(i int) error { return errs[i] })
	if idx != 1 || !errors.Is(err, first) {
		t.Fatalf("FirstErrorIndex = %d, %v", idx, err)
	}
}
