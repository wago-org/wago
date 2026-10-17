//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"sync"
	"testing"
)

func TestRV64Core3ExternrefArgumentLease(t *testing.T) {
	store := newReferenceStore(true)
	token, err := store.issueExternref("leased")
	if err != nil {
		t.Fatal(err)
	}
	in := &Instance{c: rv64Core3RefIsNullExecutionRetainedFixture(t), refStore: store}
	arguments, err := railshotBeginPublicFuncrefArguments(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := railshotStagePublicExternrefArgument(in, &arguments, token); err != nil {
		t.Fatal(err)
	}
	if err := railshotStagePublicExternrefArgument(in, &arguments, token); err != nil {
		t.Fatal("duplicate argument pin:", err)
	}
	if store.releaseExternref(token) {
		t.Fatal("release recycled a pinned externref")
	}
	if value, ok := store.resolveExternref(token); !ok || value != "leased" {
		t.Fatal("pinned externref stopped resolving", value, ok)
	}
	railshotReleasePublicFuncrefArguments(arguments)
	if arguments.scope.active || arguments.scope.externrefCount != 0 || arguments.scope.externrefStore != nil {
		t.Fatal("externref argument scope was not cleared")
	}
	if !store.releaseExternref(token) {
		t.Fatal("released invocation retained its externref pin")
	}
	reused, err := store.issueExternref("reused")
	if err != nil {
		t.Fatal(err)
	}
	if reused == token || store.pinExternref(token) {
		t.Fatal("released token authenticated after slot reuse")
	}
	if value, ok := store.resolveExternref(reused); !ok || value != "reused" {
		t.Fatal("reused externref did not resolve", value, ok)
	}
}

func TestRV64ExternrefPinReleaseRace(t *testing.T) {
	store := newReferenceStore(true)
	for i := 0; i < 128; i++ {
		token, err := store.issueExternref(i)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wait sync.WaitGroup
		var pinned, released bool
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			pinned = store.pinExternref(token)
		}()
		go func() {
			defer wait.Done()
			<-start
			released = store.releaseExternref(token)
		}()
		close(start)
		wait.Wait()
		if pinned == released {
			t.Fatalf("iteration %d: pin/release outcomes = %v/%v, want exactly one", i, pinned, released)
		}
		if pinned {
			if !store.unpinExternref(token) || !store.releaseExternref(token) {
				t.Fatalf("iteration %d: pinned winner did not release cleanly", i)
			}
		}
		reused, err := store.issueExternref(i + 1)
		if err != nil {
			t.Fatal(err)
		}
		if reused == token || store.pinExternref(token) {
			t.Fatalf("iteration %d: stale token survived generation change", i)
		}
		if !store.releaseExternref(reused) {
			t.Fatalf("iteration %d: reused token did not release", i)
		}
	}
}
