package wago

import (
	"context"
	"testing"
)

type testDirectExternRoot struct {
	value   any
	release func()
}

func (v *testDirectExternRoot) externrefPublicValue() any { return v.value }
func (v *testDirectExternRoot) releaseExternrefRoot()     { v.release() }

func TestExternRefDirectRootReleaseOutsideStoreLock(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	calls := 0
	root := &testDirectExternRoot{value: 42, release: func() {
		if !rt.refStore.mu.TryLock() {
			panic("extern root callback holds store lock")
		}
		rt.refStore.mu.Unlock()
		calls++
	}}
	token, err := rt.refStore.issueExternref(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := rt.refStore.resolveExternref(token); !ok || got != 42 {
		t.Fatal(got, ok)
	}
	if !rt.ReleaseExternRef(ExternRef{token}) || calls != 1 {
		t.Fatal("root release", calls)
	}
	if rt.ReleaseExternRef(ExternRef{token}) || calls != 1 {
		t.Fatal("root released twice", calls)
	}
}

func TestExternRefDirectRootsDrainAfterPanic(t *testing.T) {
	rt := NewRuntime()
	panicValue := &struct{}{}
	calls := 0
	for i := 0; i < 2; i++ {
		fail := i == 0
		_, err := rt.refStore.issueExternref(&testDirectExternRoot{value: i, release: func() {
			if !rt.refStore.mu.TryLock() {
				panic("extern root callback holds store lock")
			}
			rt.refStore.mu.Unlock()
			calls++
			if fail {
				panic(panicValue)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	rt.refStore.mu.Lock()
	entries := rt.refStore.releaseEntriesLocked()
	rt.refStore.mu.Unlock()
	caught, panicked := cleanupTestPanic(func() { releaseReferenceEntries(entries) })
	if !panicked || caught != panicValue || calls != 2 {
		t.Fatal("terminal root drain", caught, panicked, calls)
	}
	_ = rt.Close()
	if calls != 2 {
		t.Fatal("terminal root released twice", calls)
	}
}

func TestExternRefDirectRootsRuntimeClose(t *testing.T) {
	rt := NewRuntime()
	calls := 0
	_, err := rt.refStore.issueExternref(&testDirectExternRoot{value: 42, release: func() { calls++ }})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("completed Runtime close missed direct root", calls)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("Runtime close released direct root twice", calls)
	}
}
