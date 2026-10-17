//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
)

func TestFixedSharedDirectGoImport(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_FIXED_SHARED_GOCALL", mode)
		cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
		c, err := cfg.Compile(fixedSharedHostLoopModule(false))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var code bytes.Buffer
		if _, err = c.WriteCodeTo(&code); err != nil {
			t.Fatal(err)
		}
		found := false
		for b := code.Bytes(); len(b) >= 4; b = b[4:] {
			if binary.LittleEndian.Uint32(b) == 0xd63f0200 {
				found = true
			}
		}
		if found != (mode == "1") {
			t.Fatalf("direct Go transition=%v mode=%s", found, mode)
		}
		for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
			calls := 0
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int32) int32 { calls++; return v + 1 })
			in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run", 4)
				if err != nil || len(got) != 1 || got[0] != 10 {
					t.Fatalf("run=%v %v", got, err)
				}
			}
			if calls != 12 {
				t.Fatalf("original Go callbacks=%d", calls)
			}
			in.Close()
		}
	}
}

func TestFixedSharedDirectGoCallerScope(t *testing.T) {
	t.Setenv("WAGO_ARM64_FIXED_SHARED_GOCALL", "1")
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
		var stale []Caller
		calls := 0
		imports := NewImports()
		imports.HostFunc("env", "step", func(caller Caller, call HostCall) {
			if !caller.valid() {
				t.Fatal("inactive Caller")
			}
			stale = append(stale, caller)
			calls++
			runtime.GC()
			call.SetI32(0, call.I32(0)+1)
		}).Params(ValI32).Results(ValI32)
		in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run", 4)
		if err != nil || len(got) != 1 || got[0] != 10 || calls != 4 {
			t.Fatalf("run=%v %v calls=%d", got, err, calls)
		}
		for _, caller := range stale {
			if caller.valid() {
				t.Fatal("Caller remained valid")
			}
		}
		in.Close()
	}
}

func TestFixedSharedDirectGoImportPolicy(t *testing.T) {
	t.Setenv("WAGO_ARM64_FIXED_SHARED_GOCALL", "1")
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("direct-go-host-import", false)
	c, err := cfg.Compile(fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var code bytes.Buffer
	if _, err = c.WriteCodeTo(&code); err != nil {
		t.Fatal(err)
	}
	for b := code.Bytes(); len(b) >= 4; b = b[4:] {
		if binary.LittleEndian.Uint32(b) == 0xd63f0200 {
			t.Fatal("disabled direct-go-host-import still emits direct Go transition")
		}
	}
}
