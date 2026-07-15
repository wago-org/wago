//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedExceptionHandlingModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	catchSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	pairSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32})
	tag := []byte{0x00, 0x00} // attribute 0, type index 0
	thrower := []byte{0x20, 0x00, 0x20, 0x01, 0x08, 0x00, 0x0b}
	catcher := []byte{
		0x02, 0x02, // block (type 2): results i32, i32
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, // try_table void, catch tag 0 -> label 0
		0x20, 0x02, 0x04, 0x40, 0x00, 0x0b, // if control != 0: unreachable
		0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x00, // nested call thrower; unreachable if it returns
		0x0b,                   // end try_table
		0x0b,                   // end payload block
		0x21, 0x01, 0x21, 0x00, // preserve payload order in params 0/1
		0x20, 0x00, 0x41, 0x0a, 0x6c, 0x20, 0x01, 0x6a,
		0x0b,
	}
	uncaught := []byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, catchSig, pairSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("catch", 0, 1),
			wasmtest.ExportEntry("uncaught", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(thrower), wasmtest.Code(catcher), wasmtest.Code(uncaught))),
	)
}

func stagedExceptionHandlingTagExportModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	fnSig := wasmtest.FuncType(nil, nil)
	body := []byte{0x41, 0x01, 0x41, 0x02, 0x08, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, fnSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("tag", 4, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func stagedExceptionHandlingStartModule() []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)
	startSig := wasmtest.FuncType(nil, nil)
	tag := []byte{0x00, 0x00}
	start := []byte{0x41, 0x07, 0x41, 0x08, 0x08, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, startSig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(13, wasmtest.Vec(tag)),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(start))),
	)
}

func compileStagedExceptionHandling(t testing.TB, data []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatalf("compile staged exception handling: %v", err)
	}
	return c
}

func TestStagedExceptionHandlingLocalScalarExecution(t *testing.T) {
	data := stagedExceptionHandlingModule()
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "exception-handling") {
		t.Fatalf("public compile = %v, want closed exception-handling gate", err)
	}
	c := compileStagedExceptionHandling(t, data)
	defer c.Close()
	if !c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) {
		t.Fatal("compiled module lost exception-handling required feature")
	}
	meta := (&Module{c: c}).Metadata()
	if len(meta.Tags) != 1 || meta.Tags[0].Index != 0 || meta.Tags[0].TypeIndex != 0 || len(meta.Tags[0].Params) != 2 || meta.Tags[0].Params[0] != ValI32 || meta.Tags[0].Params[1] != ValI32 {
		t.Fatalf("tag metadata = %#v", meta.Tags)
	}
	if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), "codec v26") {
		t.Fatalf("MarshalBinary staged EH = %v, want explicit codec gate", err)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "exception-handling") {
		t.Fatalf("Capture staged EH = %v, want explicit snapshot gate", err)
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged exception handling: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("uncaught", I32(1), I32(2), I32(0)); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("initial uncaught exception result=%v err=%v", got, err)
	}
	got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
	if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
		t.Fatalf("catch result=%v err=%v, want 42", got, err)
	}
	if _, err := in.Invoke("catch", I32(4), I32(2), I32(1)); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("trap inside active try_table = %v", err)
	}
	got, err = in.Invoke("catch", I32(9), I32(7), I32(0))
	if err != nil || len(got) != 1 || uint32(got[0]) != 97 {
		t.Fatalf("post-trap catch result=%v err=%v, want 97", got, err)
	}
	if got, err := in.Invoke("uncaught", I32(1), I32(2), I32(0)); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("uncaught exception result=%v err=%v", got, err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
			panic("staged EH repeated catch failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("steady-state caught exception allocations = %v, want 0", allocs)
	}
	for i := 0; i < 10_000; i++ {
		if _, err := in.Invoke("catch", I32(4), I32(2), I32(0)); err != nil {
			t.Fatalf("repeated caught exception %d: %v", i, err)
		}
	}
}

func TestStagedExceptionHandlingStartFailsClosed(t *testing.T) {
	c := compileStagedExceptionHandling(t, stagedExceptionHandlingStartModule())
	defer c.Close()
	if in, err := instantiateCore(c, InstantiateOptions{}); err == nil {
		_ = in.Close()
		t.Fatal("uncaught start exception instantiated")
	} else if !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("start error = %v", err)
	}
}

func TestStagedExceptionHandlingProductAndPlatformGates(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	if _, err := compileWithFrontendFeatures(cfg, stagedExceptionHandlingModule(), features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard-mode staged EH = %v", err)
	}

	if _, err := compileWithFrontendFeatures(NewRuntimeConfig(), stagedExceptionHandlingTagExportModule(), features); err == nil || !strings.Contains(err.Error(), "tag exports") {
		t.Fatalf("tag-export staged EH = %v", err)
	}
}

func BenchmarkStagedExceptionHandlingCatch(b *testing.B) {
	c := compileStagedExceptionHandling(b, stagedExceptionHandlingModule())
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("catch", I32(4), I32(2), I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 42 {
			b.Fatalf("result=%v err=%v", got, err)
		}
	}
}
