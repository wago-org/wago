package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"time"
)

func TestPreparedFunctionRechecksSharedControl(t *testing.T) {
	for _, family := range []string{"variadic", "fixed", "scalar", "general"} {
		t.Run(family, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("f")
			if err != nil {
				t.Fatal(err)
			}
			call := func() ([]uint64, error) {
				switch family {
				case "fixed":
					return fn.Invoke1(41)
				case "scalar":
					return fn.invokeScalar([]uint64{41})
				case "general":
					return fn.invokeGeneral([]uint64{41})
				default:
					return fn.Invoke(41)
				}
			}
			if out, err := call(); err != nil || len(out) != 1 || out[0] != 42 {
				t.Fatalf("private call = %v, %v", out, err)
			}
			if _, err := in.ExportedFunc("f"); err != nil {
				t.Fatal(err)
			}
			if !in.nativeControlIsShared() {
				t.Fatal("export did not revoke private execution")
			}
			done := make(chan error, 1)
			nativeExecutionMu.Lock()
			go func() {
				out, err := call()
				if err == nil && (len(out) != 1 || out[0] != 42) {
					err = fmt.Errorf("result = %v", out)
				}
				done <- err
			}()
			select {
			case err := <-done:
				nativeExecutionMu.Unlock()
				t.Fatalf("shared prepared call bypassed native lease: %v", err)
			case <-time.After(20 * time.Millisecond):
				nativeExecutionMu.Unlock()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("shared prepared call did not finish")
			}
		})
	}
}

func TestPreparedOwnershipRevocationWaitsForFastEntry(t *testing.T) {
	in := &Instance{}
	if !in.lockPreparedFastState() {
		t.Fatal("private entry rejected")
	}
	started, done := make(chan struct{}), make(chan struct{})
	go func() { close(started); in.markNativeControlShared(); close(done) }()
	<-started
	select {
	case <-done:
		in.unlockPreparedFastState()
		t.Fatal("ownership published during a fast entry")
	case <-time.After(20 * time.Millisecond):
	}
	in.unlockPreparedFastState()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ownership publication did not finish")
	}
	if in.lockPreparedFastState() {
		in.unlockPreparedFastState()
		t.Fatal("shared instance admitted a fast entry")
	}
}

func TestPreparedEntryAddsNoCallAllocations(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	for _, shared := range []bool{false, true} {
		if shared {
			if _, err := in.ExportedFunc("f"); err != nil {
				t.Fatal(err)
			}
		}
		allocations := testing.AllocsPerRun(100, func() {
			out, err := fn.Invoke1(41)
			if err != nil || len(out) != 1 || out[0] != 42 {
				panic("invalid prepared result")
			}
		})
		if allocations != 0 {
			t.Fatalf("shared=%v: allocations = %v, want 0", shared, allocations)
		}
	}
}

func TestPreparedPrivateGlobalRebindsAfterExport(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0), wasmtest.ExportEntry("value", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x23, 0, 0x6a, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	if preparedPrivateEntryEnabled && preparedScalarFastEnabled && (!fn.privateFast || fn.isolatedFast) {
		t.Fatal("fixture did not select private scalar entry")
	}
	if out, err := fn.Invoke1(41); err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("private call = %v, %v", out, err)
	}
	if _, err := in.ExportedFunc("f"); err != nil {
		t.Fatal(err)
	}
	version := in.ensurePluginState().nativeContextVersion.Load()
	out, err := fn.Invoke1(41)
	if err != nil || len(out) != 1 || out[0] != 42 {
		t.Fatalf("shared call = %v, %v", out, err)
	}
	if in.ensurePluginState().nativeContextVersion.Load() <= version {
		t.Fatal("shared prepared entry did not bind native context")
	}
}

func TestPreparedConcurrentExportTransition(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("f")
	if err != nil {
		t.Fatal(err)
	}
	start, done := make(chan struct{}), make(chan error, 1)
	go func() { <-start; _, err := in.ExportedFunc("f"); done <- err }()
	close(start)
	for i := 0; i < 100; i++ {
		out, err := fn.Invoke1(41)
		if err != nil || len(out) != 1 || out[0] != 42 {
			t.Fatalf("call during export = %v, %v", out, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
