package embedded32

import (
	"errors"
	"testing"
	"unsafe"
)

func TestLinearMemoryFixedArena(t *testing.T) {
	backing := make([]byte, 3*WasmPageSize)
	for i := range backing {
		backing[i] = 0xaa
	}
	m, err := NewLinearMemory(backing, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if m.Pages() != 1 || len(m.Bytes()) != int(WasmPageSize) {
		t.Fatal("initial memory")
	}
	if !m.Bounds(WasmPageSize-4, 4) || m.Bounds(WasmPageSize-3, 4) || m.Bounds(^uint32(0)-1, 4) {
		t.Fatal("bounds")
	}
	m.Bytes()[0] = 9
	backing[WasmPageSize] = 7
	old, ok := m.Grow(1)
	if !ok || old != 1 || m.Pages() != 2 || backing[WasmPageSize] != 0 {
		t.Fatal("grow")
	}
	if old, ok = m.Grow(2); ok || old != 2 || m.Pages() != 2 {
		t.Fatal("failed grow mutated memory")
	}
	if !m.Reset(1) || m.Bytes()[0] != 0 {
		t.Fatal("reset")
	}
}

func TestCodeArenaTransactionalPublication(t *testing.T) {
	backing := make([]byte, 64)
	a := NewCodeArena(backing)
	tx, err := a.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Begin(); !errors.Is(err, ErrInvalidArena) {
		t.Fatal("concurrent transaction accepted")
	}
	b, err := tx.Allocate(12, 16)
	if err != nil {
		t.Fatal(err)
	}
	if b.Offset != 0 {
		t.Fatalf("offset=%d", b.Offset)
	}
	copy(b.Bytes, []byte{1, 2, 3})
	called := false
	if err := tx.Commit(func(off uint32, code []byte) error {
		called = true
		if off != 0 || len(code) != 12 || code[0] != 1 {
			t.Fatal("publish region")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called || a.Published() != 12 {
		t.Fatal("not published")
	}
	tx, err = a.Begin()
	if err != nil {
		t.Fatal(err)
	}
	b, err = tx.Allocate(8, 16)
	if err != nil {
		t.Fatal(err)
	}
	if b.Offset != 16 {
		t.Fatalf("aligned offset=%d", b.Offset)
	}
	b.Bytes[0] = 9
	err = tx.Commit(func(uint32, []byte) error { return errors.New("cache failure") })
	if !errors.Is(err, ErrPublish) || a.Used() != 12 || backing[16] != 0 {
		t.Fatal("publication rollback")
	}
	tx, err = a.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Allocate(128, 4); !errors.Is(err, ErrArenaCapacity) {
		t.Fatalf("capacity err=%v", err)
	}
	tx.Rollback()
}

func TestStackArenaBoundedReuseAndZero(t *testing.T) {
	a, err := NewStackArena(make([]byte, 128), 64)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := a.Acquire()
	y, _ := a.Acquire()
	if a.InUse() != 2 {
		t.Fatal("in use")
	}
	if _, err = a.Acquire(); !errors.Is(err, ErrArenaCapacity) {
		t.Fatal("capacity")
	}
	x.Bytes[0] = 7
	if !x.Release() || x.Release() {
		t.Fatal("release")
	}
	z, err := a.Acquire()
	if err != nil || z.Index != 0 || z.Bytes[0] != 0 {
		t.Fatal("reuse")
	}
	z.Release()
	y.Release()
}

func TestRuntimeInvocationLifecycle(t *testing.T) {
	memory, _ := NewLinearMemory(make([]byte, WasmPageSize), 1, 1)
	code := NewCodeArena(make([]byte, 64))
	stacks, _ := NewStackArena(make([]byte, 128), 64)
	r, err := NewRuntime(memory, code, stacks)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := r.BeginInvocation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.BeginInvocation(); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("concurrent invocation accepted")
	}
	inv.Control().RequestCancel()
	if inv.Control().Poll() != ExecutionCanceled {
		t.Fatal("cancel")
	}
	if r.Reset(1) {
		t.Fatal("reset active runtime")
	}
	if !inv.End() || inv.End() {
		t.Fatal("invocation end")
	}
	if stacks.InUse() != 0 {
		t.Fatal("stack leak")
	}
	tx, err := code.Begin()
	if err != nil {
		t.Fatal(err)
	}
	block, err := tx.Allocate(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	block.Bytes[0] = 9
	if err := tx.Commit(nil); err != nil {
		t.Fatal(err)
	}
	memory.Bytes()[0] = 8
	if !r.Reset(1) || code.Used() != 0 || memory.Bytes()[0] != 0 || r.Control.Cancel != 0 {
		t.Fatal("runtime reset")
	}
}

func TestControlAndContextABI(t *testing.T) {
	var c ControlCell
	if c.Poll() != ExecutionRunning {
		t.Fatal("running")
	}
	c.RequestCancel()
	if c.Poll() != ExecutionCanceled {
		t.Fatal("cancel")
	}
	c.Trap = 9
	c.Reset()
	if c.Trap != 0 || c.Cancel != 0 {
		t.Fatal("reset")
	}
	var x ContextABI
	checks := []struct{ got, want uintptr }{{unsafe.Offsetof(x.LinearMemoryBase), ContextLinearMemoryBaseOffset}, {unsafe.Offsetof(x.LinearMemoryLength), ContextLinearMemoryLengthOffset}, {unsafe.Offsetof(x.TrapCell), ContextTrapCellOffset}, {unsafe.Offsetof(x.CancelCell), ContextCancelCellOffset}, {unsafe.Offsetof(x.HelperTable), ContextHelperTableOffset}, {unsafe.Offsetof(x.LinearMemoryMaximum), ContextLinearMemoryMaximumOffset}, {unsafe.Offsetof(x.StackLimit), ContextStackLimitOffset}, {unsafe.Offsetof(x.GlobalsBase), ContextGlobalsBaseOffset}, {unsafe.Offsetof(x.DataSegmentsBase), ContextDataSegmentsBaseOffset}, {unsafe.Offsetof(x.DataSegmentCount), ContextDataSegmentCountOffset}, {unsafe.Offsetof(x.Table), ContextTableOffset}, {unsafe.Sizeof(x), ContextABISize}}
	for _, c := range checks {
		if c.got != c.want {
			t.Fatalf("layout got=%d want=%d", c.got, c.want)
		}
	}
}
