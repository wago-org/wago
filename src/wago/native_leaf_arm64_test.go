//go:build (linux || darwin || windows) && arm64 && !tinygo

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

//go:norace
//go:noinline
func nativeLeafIncrement(v int32) int32 { return v + 1 }

func TestNativeScalarLeafAdmissionAndValues(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callback execution")
	}
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("pure callback was not lowered")
	}
	if in.boundedHostSegments() {
		t.Fatal("native callback treated as Go safe point")
	}
	for _, v := range []int32{0, 1, -1, -2147483648, 2147483647} {
		got, err := in.Invoke("g", I32(v))
		if err != nil || len(got) != 1 || got[0] != I32(v+1) {
			t.Fatalf("g(%d) = %v, %v", v, got, err)
		}
	}
}

//go:norace
//go:noinline
func nativeLeafPolynomial(a, b int32) int32 { return a*b + a - b }

func TestNativeScalarLeafRandomizedPairAndFallback(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	c, err := Compile(NewRuntimeConfig(), returningImportModule(sig, []byte{0, 0x20, 0, 0x20, 1, 0x10, 0, 0x0b}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafPolynomial)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("polynomial not admitted")
	}
	state := uint32(0x9e3779b9)
	for i := 0; i < 2048; i++ {
		state = state*1664525 + 1013904223
		a := int32(state)
		state = state*1664525 + 1013904223
		b := int32(state)
		want := I32(nativeLeafPolynomial(a, b))
		got, err := in.Invoke("g", I32(a), I32(b))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("g(%d,%d) = %v,%v; want %d", a, b, got, err, want)
		}
	}
	calls := 0
	fallback, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", func(a, b int32) int32 { calls++; return a*b + a - b })})
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if fallback.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
		t.Fatal("side-effecting callback admitted")
	}
	got, err := fallback.Invoke("g", 7, 9)
	if err != nil || len(got) != 1 || got[0] != 61 || calls != 1 {
		t.Fatalf("fallback = %v,%v; calls %d", got, err, calls)
	}
}

func TestNativeScalarLeafLoopAllowsGCAndCancellation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, err := Compile(NewRuntimeConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 || in.boundedHostSegments() {
		t.Fatal("native loop did not retain syscall scheduling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := in.ensurePluginState()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "run", I32(0x7fffffff), 0); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for state.invokeMu.state.Load()&invocationGateHeld == 0 {
		if time.Now().After(deadline) {
			t.Fatal("invocation did not enter")
		}
		runtime.Gosched()
	}
	time.Sleep(time.Millisecond)
	// With one P, regaining this goroutine after native entry requires syscall
	// scheduling. The invocation gate remains held for the live native loop.
	collected := make(chan struct{})
	go func() { runtime.GC(); close(collected) }()
	select {
	case <-collected:
	case <-time.After(3 * time.Second):
		t.Fatal("native leaf prevented GC progress")
	}
	select {
	case err := <-done:
		t.Fatalf("long native loop ended before cancellation: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native leaf failed to cancel")
	}
}

//go:norace
//go:noinline
func nativeLeafIncrementFive(v int32) int32 { return v + 5 }

func TestNativeScalarLeafInstanceBindingsAndCrossInstance(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrementFive)})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	exported, err := first.ExportedFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", exported)})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	for i := 0; i < 100; i++ {
		for _, tc := range []struct {
			in   *Instance
			want uint64
		}{{first, 42}, {second, 46}, {relay, 42}} {
			got, err := tc.in.Invoke("g", 41)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("instance call = %v,%v; want %d", got, err, tc.want)
			}
		}
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := relay.Invoke("g", 41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("remaining shared instance = %v,%v", got, err)
	}
}

func TestNativeScalarLeafDoesNotRequireGuestSegmentProof(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	for _, enabled := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithOptimization("native-leaf-host", enabled), hostRoundtripLoopModule(t, 0))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if c.boundedHostSegments() {
			t.Fatal("conditional host loop acquired a Go segment proof")
		}
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", nativeLeafIncrement)})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if got := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; got != enabled {
			t.Fatalf("native leaf selection = %t; want %t", got, enabled)
		}
		for _, host := range []uint64{0, 1} {
			got, err := in.Invoke("run", 1024, host)
			if err != nil || len(got) != 1 || got[0] != 1024 {
				t.Fatalf("conditional loop = %v,%v", got, err)
			}
		}
	}
}

//go:norace
//go:noinline
func nativeLeafSignedShift(v int32) int32 { return v >> 13 }

//go:norace
//go:noinline
func nativeLeafWideSignedShift(v int32) int32 { return int32(int64(v) >> 33) }

//go:norace
//go:noinline
func nativeLeafLogicalShift(v int32) int32 { return int32(uint32(v) >> 13) }

//go:norace
//go:noinline
func nativeLeafBitMix(v int32) int32 { return (v << 7) ^ ^v ^ 0x12345678 }

func TestNativeScalarLeafSignedAndLogicalArithmetic(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name string
		fn   func(int32) int32
	}{
		{"signed", nativeLeafSignedShift}, {"wide-signed", nativeLeafWideSignedShift},
		{"unsigned", nativeLeafLogicalShift}, {"bit-mix", nativeLeafBitMix},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", tc.fn)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
				t.Fatal("arithmetic callback not admitted")
			}
			state := uint32(0x9e3779b9)
			for i := 0; i < 2048; i++ {
				state = state*1664525 + 1013904223
				v := int32(state)
				got, err := in.Invoke("g", I32(v))
				if err != nil || len(got) != 1 || got[0] != I32(tc.fn(v)) {
					t.Fatalf("g(%d) = %v,%v; want %d", v, got, err, tc.fn(v))
				}
			}
		})
	}
}

func TestNativeScalarLeafShortEntryIsolation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.isolatedFast {
		t.Fatal("bounded native-only export did not select isolated entry")
	}
	if !fn.directIntFast || !fn.directIntBounded {
		t.Fatal("native-only short export did not select bounded register entry")
	}
	for _, v := range []int32{0, -1, -2147483648, 2147483647} {
		for _, invoke := range []func(uint64) ([]uint64, error){func(v uint64) ([]uint64, error) { return in.Invoke("g", v) }, func(v uint64) ([]uint64, error) { return fn.Invoke(v) }} {
			got, err := invoke(I32(v))
			if err != nil || len(got) != 1 || got[0] != I32(v+1) {
				t.Fatalf("g(%d)=%v,%v", v, got, err)
			}
		}
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	if !session.state.fast {
		session.Close()
		t.Fatal("native-only session did not reserve isolated entry")
	}
	got, err := session.Invoke1(I32(-8))
	session.Close()
	if err != nil || len(got) != 1 || got[0] != I32(-7) {
		t.Fatalf("session=%v,%v", got, err)
	}
	calls := 0
	fallback, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", func(v int32) int32 { calls++; return v + 1 })})
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	fallbackFn, err := fallback.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if fallbackFn.isolatedFast || fallbackFn.directIntFast {
		t.Fatal("Go callback selected native-only isolated entry")
	}
	got, err = fallbackFn.Invoke(9)
	if err != nil || len(got) != 1 || got[0] != 10 || calls != 1 {
		t.Fatalf("fallback=%v,%v; calls=%d", got, err, calls)
	}
}

func TestNativeScalarLeafShortEntrySharingRevokesIsolation(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	c, err := Compile(NewRuntimeConfig(), sessionImportMemoryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.isolatedFast || !fn.directIntFast {
		t.Fatal("fixture lacks fast native entry")
	}
	got, err := in.Invoke("g", 41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("warm call=%v,%v", got, err)
	}
	if _, err = in.ExportedMemory("memory"); err != nil {
		t.Fatal(err)
	}
	if in.isolatedNativeScalarLeaf() || in.preparedFastStateValid() {
		t.Fatal("sharing retained native isolation")
	}
	for _, invoke := range []func() ([]uint64, error){func() ([]uint64, error) { return in.Invoke("g", 41) }, func() ([]uint64, error) { return fn.Invoke(41) }} {
		got, err := invoke()
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("revoked call=%v,%v", got, err)
		}
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.state.fast {
		t.Fatal("revoked session retained fast entry")
	}
	got, err = session.Invoke1(41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("revoked session=%v,%v", got, err)
	}
}

func TestNativeScalarLeafShortEntryArtifactDropsProof(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.boundedNativeScalarLeaf() {
		t.Fatal("fixture lacks native whole-body proof")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	if err = decoded.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	if decoded.boundedNativeScalarLeaf() || decoded.nativeScalarLeafAllowed() || decoded.directPreparedAt(0) || decoded.directPreparedBoundedAt(0) {
		t.Fatal("artifact retained native admission bits")
	}
	in, err := Instantiate(&decoded, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("g")
	if err != nil {
		t.Fatal(err)
	}
	if fn.isolatedFast || fn.directIntFast {
		t.Fatal("artifact selected native isolated entry")
	}
	got, err := fn.Invoke(41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("artifact call=%v,%v", got, err)
	}
}

func TestNativeScalarLeafBoundedEntryRollback(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	baseline, err := Compile(NewRuntimeConfig(), benchReturningImportModule())
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Close()
	for _, tc := range []struct {
		name, option             string
		isolated, direct, native bool
	}{
		{"scheduler", "prepared-bounded-entry", false, false, true},
		{"lowering", "native-leaf-host", false, false, false},
		{"register-abi", "reg-abi", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithOptimization(tc.option, false), benchReturningImportModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if tc.name == "scheduler" && !bytes.Equal(c.code, baseline.code) {
				t.Fatal("scheduler rollback changed guest code")
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", nativeLeafIncrement)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("g")
			if err != nil {
				t.Fatal(err)
			}
			if fn.isolatedFast != tc.isolated || fn.directIntFast != tc.direct || (in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0) != tc.native {
				t.Fatalf("isolation/direct/native=%t/%t/%t", fn.isolatedFast, fn.directIntFast, in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0)
			}
			for _, invoke := range []func() ([]uint64, error){func() ([]uint64, error) { return in.Invoke("g", I32(-1)) }, func() ([]uint64, error) { return fn.Invoke(I32(-1)) }} {
				got, err := invoke()
				if err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("rollback=%v,%v", got, err)
				}
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			got, err := session.Invoke1(I32(-1))
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("rollback session=%v,%v", got, err)
			}
		})
	}
}
