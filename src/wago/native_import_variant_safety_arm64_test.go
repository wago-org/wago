//go:build arm64 && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestNativeImportVariantForeignFallback(t *testing.T) {
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", "1")
	providerCode, err := NewRuntimeConfig().Compile(watToWasm(t, `(module (func (export "step") (param i32) (result i32) (i32.add (local.get 0) (i32.const 1))))`))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	provider, err := Instantiate(providerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	ref, err := provider.ExportedFunc("step")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(nativeImportVariantModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
		in, err := Instantiate(code, InstantiateOptions{Imports: testImports("env.step", ref)})
		if err != nil {
			t.Fatal(err)
		}
		if in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
			t.Fatal("foreign Wasm import admitted as native leaf")
		}
		got, err := in.Invoke("run", 17)
		if err != nil || len(got) != 1 || got[0] != 357 {
			t.Fatalf("foreign=%v,%v", got, err)
		}
		in.Close()
		if code != c {
			code.Close()
		}
	}
}

func TestNativeImportVariantTrap(t *testing.T) {
	module := watToWasm(t, `(module
 (import "env" "step" (func $a (param i32) (result i32)))
 (import "env" "step" (func $b (param i32) (result i32)))
 (func (export "run") (param $n i32) (result i32) (local $i i32) (local $sum i32)
  (loop $loop
   (local.set $sum (i32.add (local.get $sum) (call $a (call $b (local.get $i)))))
   (local.set $i (i32.add (local.get $i) (i32.const 1)))
   (br_if $loop (i32.lt_u (local.get $i) (local.get $n))))
  (i32.div_u (local.get $sum) (i32.const 0))))`)
	var images [2][]byte
	for mode, flag := range []string{"0", "1"} {
		t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", flag)
		c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[mode] = append([]byte(nil), image.Bytes()...)
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.step", nativeLeafIncrement)})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			_, err = in.Invoke("run", 17)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapDivZero {
				t.Fatalf("mode=%s trap=%v", flag, err)
			}
		}
		in.Close()
	}
	if !codeProfileEnabled && bytes.Equal(images[0], images[1]) {
		t.Fatal("trap fixture did not exercise guarded body")
	}
}

func TestNativeImportVariantCancellationAndGC(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", "1")
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(nativeImportVariantModule(false))
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
