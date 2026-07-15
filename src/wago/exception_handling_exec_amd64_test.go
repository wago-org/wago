//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
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

func stagedExceptionHandlingGeneralModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}),
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	tags := make([][]byte, 6)
	for i := range tags {
		tags[i] = []byte{0x00, byte(i)}
	}
	throwers := [][]byte{
		{0x08, 0x00, 0x0b},
		{0x20, 0x00, 0x08, 0x01, 0x0b},
		{0x20, 0x00, 0x08, 0x02, 0x0b},
		{0x20, 0x00, 0x08, 0x03, 0x0b},
		{0x20, 0x00, 0x08, 0x04, 0x0b},
		{0x20, 0x00, 0x20, 0x01, 0x08, 0x05, 0x0b},
	}
	echo := func(tag, thrower byte, blockType byte, zero []byte) []byte {
		body := []byte{0x02, blockType, 0x1f, blockType, 0x01, 0x00, tag, 0x00, 0x20, 0x00, 0x10, thrower}
		body = append(body, zero...)
		return append(body, 0x0b, 0x0b, 0x0b)
	}
	pair := []byte{
		0x02, 0x0a, // block type 10: (result i32 i64)
		0x1f, 0x0a, 0x01, 0x00, 0x05, 0x00,
		0x41, 0x0b, 0x42, 0x16, 0x10, 0x05,
		0x41, 0x00, 0x42, 0x00,
		0x0b, 0x0b, 0x0b,
	}
	ordered := []byte{
		0x02, 0x7f, // outer i32
		0x02, 0x40, // catch-all target
		0x1f, 0x40, 0x03,
		0x00, 0x01, 0x01, // catch tag 1 -> outer
		0x00, 0x00, 0x00, // catch tag 0 -> catch-all target
		0x02, 0x00, // catch_all -> catch-all target
		0x20, 0x00, 0x45, 0x04, 0x40,
		0x41, 0x37, 0x10, 0x01, // selector 0: tag 1 payload 55
		0x05,
		0x20, 0x00, 0x41, 0x01, 0x46, 0x04, 0x40,
		0x10, 0x00, // selector 1: tag 0
		0x05,
		0x42, 0x09, 0x10, 0x02, // other: tag 2, caught by catch_all
		0x0b, 0x0b,
		0x41, 0x07, 0x0c, 0x02, // normal result (unreached by these cases)
		0x0b, // try_table
		0x0b, // catch-all target
		0x41, 0x09,
		0x0b, 0x0b,
	}
	nested := []byte{
		0x02, 0x40,
		0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, // outer catches tag 0
		0x02, 0x7f,
		0x1f, 0x7f, 0x01, 0x00, 0x01, 0x00, // inner catches tag 1 payload
		0x10, 0x00, 0x41, 0x00, // throw tag 0; fallback satisfies normal result
		0x0b, 0x0b, 0x1a, 0x0b,
		0x41, 0x01, 0x0f,
		0x0b,
		0x41, 0x02, 0x0b,
	}
	sequential := []byte{
		0x02, 0x40, 0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, 0x10, 0x00, 0x0b, 0x0b,
		0x02, 0x40, 0x1f, 0x40, 0x01, 0x00, 0x00, 0x00, 0x10, 0x00, 0x0b, 0x0b,
		0x41, 0x07, 0x0b,
	}
	bodies := append(throwers,
		echo(1, 1, 0x7f, []byte{0x41, 0x00}),
		echo(2, 2, 0x7e, []byte{0x42, 0x00}),
		echo(3, 3, 0x7d, []byte{0x43, 0, 0, 0, 0}),
		echo(4, 4, 0x7c, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0}),
		pair, ordered, nested, sequential,
	)
	funcTypes := [][]byte{{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7}, {8}, {9}, {10}, {6}, {11}, {11}}
	codes := make([][]byte, len(bodies))
	for i := range bodies {
		codes[i] = wasmtest.Code(bodies[i])
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcTypes...)),
		wasmtest.Section(13, wasmtest.Vec(tags...)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("i32", 0, 6),
			wasmtest.ExportEntry("i64", 0, 7),
			wasmtest.ExportEntry("f32", 0, 8),
			wasmtest.ExportEntry("f64", 0, 9),
			wasmtest.ExportEntry("pair", 0, 10),
			wasmtest.ExportEntry("ordered", 0, 11),
			wasmtest.ExportEntry("nested", 0, 12),
			wasmtest.ExportEntry("sequential", 0, 13),
		)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
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

func stagedExceptionReferenceModule() []byte {
	voidSig := wasmtest.FuncType(nil, nil)
	exnSig := []byte{0x60, 0x00, 0x01, 0x63, 0x69} // () -> (ref null exn)
	thrower := []byte{0x08, 0x00, 0x0b}
	rethrow := []byte{
		0x02, 0x01, // block type 1: (result exnref)
		0x1f, 0x40, 0x01, 0x01, 0x00, 0x00, // try_table void, catch_ref tag 0 -> label 0
		0x10, 0x00, // nested local thrower
		0x0b, // end try_table
		0x00, // normal fallthrough is unreachable
		0x0b, // end block with rooted exnref
		0x0a, // throw_ref
		0x0b,
	}
	nullThrow := []byte{0xd0, 0x69, 0x0a, 0x0b} // ref.null exn; throw_ref
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(voidSig, exnSig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00}, []byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("rethrow", 0, 1),
			wasmtest.ExportEntry("null", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(thrower), wasmtest.Code(rethrow), wasmtest.Code(nullThrow))),
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
	return compileStagedExceptionHandlingFeatures(t, data, false)
}

func compileStagedExceptionHandlingFeatures(t testing.TB, data []byte, exceptionReferences bool) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = exceptionReferences
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
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary staged EH: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary staged EH: %v", err)
	}
	if got := (&Module{c: &loaded}).Metadata().Tags; !reflect.DeepEqual(got, meta.Tags) {
		t.Fatalf("reloaded EH tag metadata = %#v, want %#v", got, meta.Tags)
	}
	_ = loaded.Close()
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

func TestStagedExceptionHandlingGeneralLocalScalarExecution(t *testing.T) {
	c := compileStagedExceptionHandling(t, stagedExceptionHandlingGeneralModule())
	defer c.Close()
	meta := (&Module{c: c}).Metadata()
	if len(meta.Tags) != 6 {
		t.Fatalf("tag metadata count = %d, want 6", len(meta.Tags))
	}
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate generalized staged EH: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("pair"); err != nil || len(got) != 2 || uint32(got[0]) != 11 || got[1] != 22 {
		t.Fatalf("pair result=%v err=%v, want [11 22]", got, err)
	}
	for _, tc := range []struct {
		name string
		arg  uint64
		want uint64
	}{
		{name: "i32", arg: I32(-17), want: I32(-17)},
		{name: "i64", arg: I64(-0x123456789), want: I64(-0x123456789)},
		{name: "f32", arg: F32(10.5), want: F32(10.5)},
		{name: "f64", arg: F64(-19.25), want: F64(-19.25)},
	} {
		got, err := in.Invoke(tc.name, tc.arg)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s result=%v err=%v, want %#x", tc.name, got, err, tc.want)
		}
	}
	for selector, want := range []uint32{55, 9, 9} {
		got, err := in.Invoke("ordered", I32(int32(selector)))
		if err != nil || len(got) != 1 || uint32(got[0]) != want {
			t.Fatalf("ordered(%d) result=%v err=%v, want %d", selector, got, err, want)
		}
	}
	if got, err := in.Invoke("nested"); err != nil || len(got) != 1 || uint32(got[0]) != 2 {
		t.Fatalf("nested result=%v err=%v, want 2", got, err)
	}
	if got, err := in.Invoke("sequential"); err != nil || len(got) != 1 || uint32(got[0]) != 7 {
		t.Fatalf("sequential result=%v err=%v, want 7", got, err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("ordered", I32(0))
		if err != nil || len(got) != 1 || uint32(got[0]) != 55 {
			panic("general staged EH repeated catch failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("general caught exception allocations = %v, want 0", allocs)
	}
}

func TestStagedExceptionHandlingRootedReferenceNestedCallAndNullTrap(t *testing.T) {
	data := stagedExceptionReferenceModule()
	if _, err := compileStagedExceptionHandlingFeaturesForTest(data, false); err == nil || !strings.Contains(err.Error(), "exception-reference") {
		t.Fatalf("exception references without private gate = %v", err)
	}
	c := compileStagedExceptionHandlingFeatures(t, data, true)
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate rooted exception references: %v", err)
	}
	defer in.Close()
	if got, err := in.Invoke("rethrow"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("nested catch_ref/throw_ref result=%v err=%v", got, err)
	}
	if got, err := in.Invoke("null"); err == nil || !strings.Contains(err.Error(), "null reference") {
		t.Fatalf("null throw_ref result=%v err=%v", got, err)
	}
	for i := 0; i < 10_000; i++ {
		if _, err := in.Invoke("rethrow"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
			t.Fatalf("repeated nested rethrow %d: %v", i, err)
		}
	}
}

func compileStagedExceptionHandlingFeaturesForTest(data []byte, exceptionReferences bool) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = exceptionReferences
	return compileWithFrontendFeatures(cfg, data, features)
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

	exported, err := compileWithFrontendFeatures(NewRuntimeConfig(), stagedExceptionHandlingTagExportModule(), features)
	if err != nil {
		t.Fatalf("tag-export staged EH compile: %v", err)
	}
	_ = exported.Close()
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
