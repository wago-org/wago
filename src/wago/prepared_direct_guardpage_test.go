//go:build wago_guardpage && (amd64 || arm64) && (linux || darwin || windows)

package wago

import "testing"

func TestPreparedDirectMemoryFreeEntryWithSignalBounds(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased), benchAddOneModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("memory-free function did not retain the direct-entry proof")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast || !fn.directIsolated {
		t.Fatalf("signal-bounds direct/isolated selection = %v/%v, want true/true", fn.directIntFast, fn.directIsolated)
	}
	got, err := fn.Invoke1(I32(41))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("invoke = %v, %v; want [42], nil", got, err)
	}
}
