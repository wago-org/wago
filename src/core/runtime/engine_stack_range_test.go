package runtime

import "testing"

func TestNativeStackRangeBoundsAndAlias(t *testing.T) {
	e := &Engine{stack: make([]byte, stackFenceMargin+64)}
	base, limit, top := slicePtr(e.stack), e.StackLimit(), e.StackTop()
	for _, tc := range []struct {
		address uintptr
		size    int
	}{
		{0, 1}, {base - 1, 1}, {limit - 1, 1}, {limit, 0},
		{limit, -1}, {top, 1}, {top - 1, 2}, {^uintptr(0), 1},
	} {
		if got := e.NativeStackRange(tc.address, tc.size); got != nil {
			t.Fatalf("accepted address %#x, size %d", tc.address, tc.size)
		}
	}
	if (*Engine)(nil).NativeStackRange(limit, 1) != nil || new(Engine).NativeStackRange(limit, 1) != nil {
		t.Fatal("accepted missing stack")
	}
	for _, tc := range []struct {
		address uintptr
		size    int
	}{{limit, 64}, {top - 1, 1}} {
		got := e.NativeStackRange(tc.address, tc.size)
		if len(got) != tc.size || cap(got) != tc.size || slicePtr(got) != tc.address {
			t.Fatal("stack range changed shape or identity")
		}
		got[0] = 71
		if e.stack[int(tc.address-base)] != 71 {
			t.Fatal("stack range lost writable alias")
		}
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if len(e.NativeStackRange(limit, 64)) != 64 {
			t.Fatal("stack range unavailable")
		}
	}); allocs != 0 {
		t.Fatalf("stack range allocated %g times", allocs)
	}
}
