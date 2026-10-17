//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestSharedSingleEntryGuardAndGoFallback(t *testing.T) {
	testSharedEntryCalls(t, "WAGO_ARM64_SHARED_IMPORT_VARIANT")
}
func TestSharedEntryRegisterResultAndGoFallback(t *testing.T) {
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
	testSharedEntryCalls(t, "WAGO_ARM64_SHARED_IMPORT_RESULT_REG")
}
func TestSharedEntryRegisterArgumentAndGoFallback(t *testing.T) {
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
	testSharedEntryCalls(t, "WAGO_ARM64_SHARED_IMPORT_ARG_REG")
}
func testSharedEntryCalls(t *testing.T, selector string) {
	images := map[string][]byte{}
	for _, mode := range []string{"0", "1"} {
		t.Setenv(selector, mode)
		cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
		c, err := cfg.Compile(fixedSharedHostLoopModule(false))
		if err != nil {
			t.Fatal(err)
		}
		var image bytes.Buffer
		if _, err = c.WriteCodeTo(&image); err != nil {
			t.Fatal(err)
		}
		images[mode] = append([]byte(nil), image.Bytes()...)
		for index, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
			for _, kind := range []string{"go", "leaf", "pure-helper", "bits"} {
				native := kind == "bits" || kind == "leaf"
				calls := 0
				var callback any = nativeLeafIncrement
				if kind == "bits" {
					callback = nativeLeafBitMix
				}
				if kind == "pure-helper" {
					callback = sharedNativePinsPureHelper
				}
				if kind == "go" {
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
					if kind == "bits" {
						var sum uint32
						for i := uint64(0); i < count; i++ {
							sum += uint32(nativeLeafBitMix(int32(i)))
						}
						want = uint64(sum)
					}
					got, err := in.Invoke("run", uint64(n))
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("mode=%s native=%t artifact=%d n=%d got=%v,%v want=%d", mode, native, index, n, got, err, want)
					}
					wantCalls += int(count)
					if kind == "go" && calls != wantCalls {
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
		t.Fatal("entry guard selector did not change shared loop")
	}
}

func TestSharedEntryGuardForeignFallback(t *testing.T) {
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", "1")
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
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
	c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedHostLoopModule(false))
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
		if err != nil || len(got) != 1 || got[0] != 153 {
			t.Fatalf("foreign=%v,%v", got, err)
		}
		in.Close()
		if code != c {
			code.Close()
		}
	}
}

func TestSharedEntryGuardCancellationAndGC(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains Go callbacks")
	}
	t.Setenv("WAGO_ARM64_NATIVE_IMPORT_VARIANT", "1")
	t.Setenv("WAGO_ARM64_SHARED_IMPORT_VARIANT", "1")
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	c, err := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).Compile(fixedSharedHostLoopModule(false))
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
