//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
	"testing"
	"time"
)

func TestFixedSharedStoreForwardSourceArtifactAndCallbacks(t *testing.T) {
	images := map[string][]byte{}
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
		c, err := cfg.Compile(fixedSharedForwardLoopModule(0))
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[mode] = append([]byte(nil), image.Bytes()...)
		for index, code := range []*Compiled{c} {
			for _, native := range []bool{false, true} {
				calls := 0
				var callback any = nativeLeafIncrement
				if !native {
					callback = func(v int32) int32 { calls++; return v + 1 }
				}
				in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", callback)})
				if err != nil {
					t.Fatal(err)
				}
				if actual := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; actual != (native && index == 0 && !codeProfileEnabled) {
					t.Fatalf("native=%t artifact=%d admitted=%t", native, index, actual)
				}
				wantCalls := 0
				for _, n := range []uint32{0, 1, 17, 129, 65536} {
					count := uint64(n)
					if count == 0 {
						count = 1
					}
					want := uint64(uint32(count * (count + 1) / 2))
					got, err := in.Invoke("run", uint64(n))
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("mode=%s native=%t artifact=%d n=%d got=%v,%v want=%d", mode, native, index, n, got, err, want)
					}
					wantCalls += int(count)
					if !native && calls != wantCalls {
						t.Fatalf("calls=%d want=%d", calls, wantCalls)
					}
				}
				in.Close()
			}
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
	if !codeProfileEnabled && bytes.Equal(images["0"], images["1"]) {
		t.Fatal("shared forwarding selector did not change loop")
	}
}

func fixedSharedForwardLoopModule(off uint32) []byte {
	memarg := append([]byte{2}, wasmtest.ULEB(off)...)
	body := []byte{1, 2, 0x7f}
	if off == 65533 {
		body = append(body, 0x41, 0x7f, 0x21, 2)
	}
	body = append(body, 3, 0x40, 0x20, 1, 0x41, 0, 0x20, 2, 0x36)
	body = append(body, memarg...)
	body = append(body, 0x41, 0, 0x28)
	body = append(body, memarg...)
	body = append(body, 0x10, 0, 0x6a, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(importEntry("env", "step", 0, 0))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}
func TestFixedSharedStoreForwardBoundsAndEffects(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		for _, off := range []uint32{65532, 65533} {
			c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedForwardLoopModule(off))
			if err != nil {
				t.Fatal(err)
			}
			for _, code := range []*Compiled{c} {
				calls := 0
				in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", func(v int32) int32 { calls++; return v + 1 })})
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("run", 17)
				if off == 65532 {
					if err != nil || len(got) != 1 || got[0] != 153 || calls != 17 {
						t.Fatalf("valid mode=%s got=%v err=%v calls=%d", mode, got, err, calls)
					}
				} else {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds || calls != 0 {
						t.Fatalf("OOB mode=%s err=%v calls=%d", mode, err, calls)
					}
					tail, ok := in.Read(65533, 3)
					if !ok || !bytes.Equal(tail, []byte{0, 0, 0}) {
						t.Fatalf("partial OOB write: %x,%t", tail, ok)
					}
				}
				in.Close()
				if code != c {
					code.Close()
				}
			}
			c.Close()
		}
	}
}

func TestFixedSharedStoreForwardLeavesI64Unchanged(t *testing.T) {
	body := []byte{0, 0x20, 0, 0x20, 1, 0x37, 3, 0, 0x20, 0, 0x29, 3, 0, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	var images [2][]byte
	for index, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[index] = append([]byte(nil), image.Bytes()...)
		for _, code := range []*Compiled{c} {
			in, err := Instantiate(code, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, address := range []uint64{0, 1, 65528} {
				for _, want := range []uint64{0, 0xfedcba9876543210, ^uint64(0)} {
					got, err := in.Invoke("run", address, want)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("mode=%s at=%d got=%x err=%v want=%x", mode, address, got, err, want)
					}
				}
			}
			in.Close()
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
	if !bytes.Equal(images[0], images[1]) {
		t.Fatal("i32-only forwarding changed I64 accesses")
	}
}

func TestFixedSharedStoreForwardI32Bits(t *testing.T) {
	body := []byte{0, 0x20, 0, 0x20, 1, 0x36, 2, 0, 0x20, 0, 0x28, 2, 0, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	var images [2][]byte
	for index, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[index] = append([]byte(nil), image.Bytes()...)
		for _, code := range []*Compiled{c} {
			in, err := Instantiate(code, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, address := range []uint64{0, 1, 65532, 0x100000001} {
				for _, want := range []uint64{0, 0x80000000, 0xfedcba9876543210, ^uint64(0)} {
					got, err := in.Invoke("run", address, want)
					if err != nil || len(got) != 1 || got[0] != uint64(uint32(want)) {
						t.Fatalf("mode=%s at=%d got=%x err=%v want=%x", mode, address, got, err, want)
					}
				}
			}
			in.Close()
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
	if !codeProfileEnabled && bytes.Equal(images[0], images[1]) {
		t.Fatal("I32 forwarding did not fire")
	}
}

func TestFixedSharedStoreForwardProfilingPolicy(t *testing.T) {
	if !codeProfileEnabled {
		t.Skip("profiling build only")
	}
	var images [2][]byte
	for i, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedForwardLoopModule(0))
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[i] = append([]byte(nil), image.Bytes()...)
		c.Close()
	}
	if !bytes.Equal(images[0], images[1]) {
		t.Fatal("profiling still forwards a shared memory read")
	}
}
func TestFixedSharedStoreForwardCallbackBarrier(t *testing.T) {
	body := []byte{0, 0x41, 0, 0x41, 7, 0x36, 2, 0, 0x10, 0, 0x41, 0, 0x28, 2, 0, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(importEntry("env", "touch", 0, 0))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []*Compiled{c} {
			var in *Instance
			calls := 0
			in, err = Instantiate(code, InstantiateOptions{Imports: testImports("env.touch", func() { calls++; in.Memory().UnsafeBytes()[0] = 9 })})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 9 || calls != 1 {
				t.Fatalf("mode=%s got=%v err=%v calls=%d", mode, got, err, calls)
			}
			in.Close()
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
}

func TestFixedSharedStoreForwardAddressLocalMutation(t *testing.T) {
	// The store address is evaluated before the value overwrites that local.
	body := []byte{0, 0x20, 0, 0x20, 1, 0x22, 0, 0x36, 2, 0, 0x20, 0, 0x28, 2, 0, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []*Compiled{c} {
			in, err := Instantiate(code, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run", 0, 16)
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("mode=%s got=%v,%v", mode, got, err)
			}
			stored, ok := in.Read(0, 4)
			if !ok || !bytes.Equal(stored, []byte{16, 0, 0, 0}) {
				t.Fatalf("store missing: %x", stored)
			}
			in.Close()
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
}

func TestFixedSharedStoreForwardNativeBounds(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", mode)
		t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
		c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedForwardLoopModule(65533))
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []*Compiled{c} {
			in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", nativeLeafIncrement)})
			if err != nil {
				t.Fatal(err)
			}
			_, err = in.Invoke("run", 17)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatalf("native bounds mode=%s: %v", mode, err)
			}
			tail, ok := in.Read(65533, 3)
			if !ok || !bytes.Equal(tail, []byte{0, 0, 0}) {
				t.Fatalf("partial store: %x", tail)
			}
			in.Close()
			if code != c {
				code.Close()
			}
		}
		c.Close()
	}
}

func TestFixedSharedStoreForwardNativeCancellationAndGC(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", "1")
	t.Setenv("WAGO_ARM64_SHARED_STORE_FORWARD", "1")
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_ARG_REG", "1")
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_RESULT_REG", "1")
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedForwardLoopModule(0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", nativeLeafIncrement)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.executionFlags.Load()&executionFlagNativeScalarLeaf == 0 {
		t.Fatal("native body not admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := in.ensurePluginState()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "run", uint64(0x7fffffff)); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for state.invokeMu.state.Load()&invocationGateHeld == 0 {
		if time.Now().After(deadline) {
			t.Fatal("invocation did not enter")
		}
		runtime.Gosched()
	}
	time.Sleep(time.Millisecond)
	collected := make(chan struct{})
	go func() { runtime.GC(); close(collected) }()
	select {
	case <-collected:
	case <-time.After(3 * time.Second):
		t.Fatal("native loop prevented GC progress")
	}
	select {
	case err := <-done:
		t.Fatalf("loop exited before cancel: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native loop did not cancel")
	}
}
