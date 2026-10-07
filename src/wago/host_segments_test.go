//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func hostYieldLoopModule() []byte {
	body := []byte{
		1, 1, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0, 0x45, 0x0d, 1,
		0x20, 2, 0x10, 0, 0x21, 2,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "step", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func goHostSegmentConfig() *RuntimeConfig {
	cfg := NewRuntimeConfig()
	if runtime.GOARCH == "arm64" {
		cfg = cfg.WithOptimization("native-leaf-host", false)
	}
	return cfg
}

func TestBoundedHostYieldAdmissionAndRollback(t *testing.T) {
	data := hostYieldLoopModule()
	bounded, err := Compile(goHostSegmentConfig(), data)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	unbounded, err := Compile(goHostSegmentConfig().WithOptimization("prepared-bounded-entry", false), data)
	if err != nil {
		t.Fatal(err)
	}
	defer unbounded.Close()
	if !bounded.boundedHostSegments() || unbounded.boundedHostSegments() {
		t.Fatalf("segment selection = %t/%t", bounded.boundedHostSegments(), unbounded.boundedHostSegments())
	}
	if bounded.directPreparedBoundedAt(0) {
		t.Fatal("host loop received a whole-entry bound")
	}
	if !bytes.Equal(bounded.code, unbounded.code) {
		t.Fatal("scheduler proof changed native guest code")
	}
	for _, c := range []*Compiled{bounded, unbounded} {
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", func(v int32) int32 { return v + 1 })})
		if err != nil {
			t.Fatal(err)
		}
		if !in.hasSingleDirectTypedScalarHost() {
			t.Fatal("typed callback not admitted")
		}
		for _, count := range []int32{0, 1, 1024, 65536} {
			got, err := in.Invoke("run", I32(count), 0)
			if err != nil || len(got) != 1 || got[0] != uint64(count) {
				t.Fatalf("run(%d) = %v, %v", count, got, err)
			}
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		s, err := fn.OpenSession()
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Invoke2(1024, 0)
		if err != nil || len(got) != 1 || got[0] != 1024 {
			t.Fatalf("session = %v, %v", got, err)
		}
		s.Close()
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	conditional, err := Compile(NewRuntimeConfig(), hostRoundtripLoopModule(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer conditional.Close()
	if conditional.boundedHostSegments() {
		t.Fatal("loop can bypass its host call")
	}
}

func TestBoundedHostYieldAllowsGCAndCancellationWithOneP(t *testing.T) {
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) { testBoundedHostYieldGCAndCancellation(t, kind) })
	}
}

func testBoundedHostYieldGCAndCancellation(t *testing.T, kind string) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, err := Compile(NewRuntimeConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.boundedHostSegments() {
		t.Fatal("fixture has no host segment proof")
	}
	started := make(chan struct{})
	first := true
	step := func(v int32) int32 {
		if first {
			first = false
			close(started)
		}
		return v + 1
	}
	var callback any = step
	switch kind {
	case "HostCall":
		callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
	case "Caller":
		callback = func(_ Caller, call HostCall) { call.SetI32(0, step(call.I32(0))) }
	}
	imports := NewImports()
	imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "run", I32(0x7fffffff), 0); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("host loop did not start")
	}
	collected := make(chan struct{})
	go func() { runtime.GC(); close(collected) }()
	select {
	case <-collected:
	case <-time.After(3 * time.Second):
		t.Fatal("bounded host segments prevented GC progress")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bounded host loop did not cancel")
	}
}

func TestBoundedHostYieldArtifactDropsCompilerProof(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.boundedHostSegments() {
		t.Fatal("fixture lacks proof")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err := decoded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	if decoded.boundedHostSegments() || decoded.nativeScalarLeafAllowed() || decoded.boundedNativeScalarLeaf() {
		t.Fatal("artifact retained compiler-only scheduler proof")
	}
	in, err := Instantiate(&decoded, InstantiateOptions{Imports: testImports("env.step", func(v int32) int32 { return v + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run", 1024, 0)
	if err != nil || len(got) != 1 || got[0] != 1024 {
		t.Fatalf("decoded call = %v, %v", got, err)
	}
}

func TestBoundedCallerHostYieldReentryAndExpiry(t *testing.T) {
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.boundedHostSegments() {
		t.Fatal("fixture lacks segment proof")
	}
	var in *Instance
	var outer, nested Caller
	imports := NewImports()
	imports.HostFunc("env", "f", func(caller Caller, call HostCall) {
		if !isNativeActive(in, caller.invocationID) {
			t.Fatal("Caller callback has no active native identity")
		}
		if call.I32(0) == 1 {
			outer = caller
			got, err := in.InvokeFromHost(context.Background(), caller, "g", 0)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("nested = %v, %v", got, err)
			}
			if !outer.valid() || nested.valid() {
				t.Fatal("nested scope was not restored/expired")
			}
			if !isNativeActive(in, caller.invocationID) {
				t.Fatal("outer native identity not restored after reentry")
			}
		} else {
			nested = caller
			if outer.valid() {
				t.Fatal("outer scope remained valid in nested callback")
			}
			runtime.GC()
		}
		call.SetI32(0, call.I32(0)+1)
	}).Params(ValI32).Results(ValI32)
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.hasBoundedCallerHostView() {
		t.Fatal("Caller portal was not admitted")
	}
	for i := 0; i < 2; i++ {
		got, err := in.Invoke("g", 1)
		if err != nil || len(got) != 1 || got[0] != 2 {
			t.Fatalf("outer = %v, %v", got, err)
		}
		if outer.valid() || nested.valid() {
			t.Fatal("retained caller remained valid")
		}
		if isNativeActive(in, outer.invocationID) {
			t.Fatal("native identity remained active after callback")
		}
		if _, err := in.InvokeFromHost(context.Background(), outer, "g", 0); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("retained reentry = %v", err)
		}
	}
}

func TestBoundedHostYieldAllocations(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name string
		fn   any
	}{
		{"typed", func(v int32) int32 { return v + 1 }},
		{"HostCall", func(call HostCall) { call.SetI32(0, call.I32(0)+1) }},
		{"Caller", func(_ Caller, call HostCall) { call.SetI32(0, call.I32(0)+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imports := NewImports()
			imports.HostFunc("env", "step", tc.fn).Params(ValI32).Results(ValI32)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got := testing.AllocsPerRun(1000, func() {
				results, err := in.Invoke("run", 64, 0)
				if err != nil || len(results) != 1 || results[0] != 64 {
					t.Fatalf("invoke = %v, %v", results, err)
				}
			}); got != 0 {
				t.Fatalf("allocations = %g; want 0", got)
			}
		})
	}
}
