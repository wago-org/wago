//go:build (linux || darwin) && (amd64 || arm64) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// These tests adapt portable runtime and compiler regressions from Wasmtime's
// tests/all at revision a5720e50d5ec9eab34eed690eee952abfdd0e3ba.
// Port of tests/all/pooling_allocator.rs::memory_zeroed.
func TestWasmtimePortReusedMemoryIsZeroed(t *testing.T) {
	compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(wasmtimeReuseMemoryModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	first, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	memory := first.Memory().Bytes()
	for i := range memory {
		memory[i] = 0xfe
	}
	reused := first.jm
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.jm != reused {
		t.Fatalf("JobMemory cache did not reuse the dirtied mapping: first=%p second=%p", reused, second.jm)
	}
	for i, b := range second.Memory().Bytes() {
		if b != 0 {
			t.Fatalf("reused memory byte %d = %#x, want zero", i, b)
		}
	}
}

// Port of tests/all/pooling_allocator.rs::table_zeroed.
func TestWasmtimePortReusedFuncrefTableIsZeroed(t *testing.T) {
	compiled := MustCompile(wasmtimeReuseTableModule())
	defer compiled.Close()
	first, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	for i := int32(0); i < 10; i++ {
		if _, err := first.Invoke("set", I32(i)); err != nil {
			t.Fatalf("seed table[%d]: %v", i, err)
		}
	}
	reused := first.ar
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.ar != reused {
		t.Fatalf("arena cache did not reuse the dirtied table arena: first=%p second=%p", reused, second.ar)
	}
	for i := int32(0); i < 10; i++ {
		got, err := second.Invoke("is_null", I32(i))
		if err != nil || len(got) != 1 || AsI32(got[0]) != 1 {
			t.Fatalf("reused table[%d] null check = %v, %v; want 1", i, got, err)
		}
	}
}

// Port of tests/all/pooling_allocator.rs::memory_reset_if_instantiation_fails.
func TestWasmtimePortFailedInstantiationMemoryDoesNotLeak(t *testing.T) {
	clean, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(wasmtimeReuseMemoryModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Close()
	failing, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(wasmtimeReuseMemoryModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer failing.Close()

	prime, err := Instantiate(clean)
	if err != nil {
		t.Fatal(err)
	}
	reused := prime.jm
	if err := prime.Close(); err != nil {
		t.Fatal(err)
	}
	if in, err := Instantiate(failing); in != nil || err == nil {
		if in != nil {
			_ = in.Close()
		}
		t.Fatalf("failing data-image instantiation = %p, %v; want start trap", in, err)
	}

	after, err := Instantiate(clean)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if after.jm != reused {
		t.Fatalf("failed instantiation did not return the primed mapping: prime=%p after=%p", reused, after.jm)
	}
	for i, b := range after.Memory().Bytes() {
		if b != 0 {
			t.Fatalf("memory byte %d after failed instantiation = %#x, want zero", i, b)
		}
	}
}

// Port of tests/all/module.rs::large_add_chain_no_stack_overflow.
func TestWasmtimePortLargeAddChainDoesNotOverflowCompilerStack(t *testing.T) {
	const additions = 20_000
	body := make([]byte, 0, 3+additions*3)
	body = append(body, 0x42, 0x01) // i64.const 1
	for i := 0; i < additions; i++ {
		body = append(body, 0x42, 0x01, 0x7c) // i64.const 1; i64.add
	}
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("compile %d-add chain: %v", additions, err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("sum")
	if err != nil || len(got) != 1 || AsI64(got[0]) != additions+1 {
		t.Fatalf("sum() = %v, %v; want %d", got, err, additions+1)
	}
}

// Port of tests/all/module.rs::validate_deterministic.
func TestWasmtimePortParallelValidationErrorIsDeterministic(t *testing.T) {
	const functions = 384
	funcTypes := make([][]byte, functions)
	bodies := make([][]byte, functions)
	for i := 0; i < functions; i++ {
		funcTypes[i] = wasmtest.ULEB(0)
		// i64.add receives i32/i64 instead of i64/i64.
		bodies[i] = wasmtest.Code([]byte{0x41, 0x00, 0x42, 0x01, 0x7c, 0x0b})
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(funcTypes...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)

	var want string
	for _, workers := range []int{1, 2, 8, 0} {
		for attempt := 0; attempt < 3; attempt++ {
			compiled, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(mod)
			if compiled != nil {
				_ = compiled.Close()
			}
			if err == nil {
				t.Fatalf("workers=%d attempt=%d accepted invalid module", workers, attempt)
			}
			if want == "" {
				want = err.Error()
			} else if got := err.Error(); got != want {
				t.Fatalf("workers=%d attempt=%d error changed:\n got: %s\nwant: %s", workers, attempt, got, want)
			}
		}
	}
}

// Port of tests/all/import_indexes.rs::same_import_names_still_distinct.
func TestWasmtimePortSameNamedImportsRemainDistinctByIndex(t *testing.T) {
	i32Result := wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I32})
	f32Result := wasmtest.FuncType(nil, []corewasm.ValType{corewasm.F32})
	imports := [][]byte{
		append(append(append(wasmtest.Name(""), wasmtest.Name("")...), 0x00), wasmtest.ULEB(0)...),
		append(append(append(wasmtest.Name(""), wasmtest.Name("")...), 0x00), wasmtest.ULEB(1)...),
	}
	modBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(i32Result, f32Result)),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x10, 0x01, 0xa9, 0x6a, 0x0b}))),
	)
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(modBytes)
	if err != nil {
		t.Fatal(err)
	}
	decls := mod.Imports()
	if len(decls) != 2 || decls[0].Index != 0 || decls[1].Index != 1 || decls[0].Module != "" || decls[0].Name != "" || decls[1].Module != "" || decls[1].Name != "" || decls[0].Results[0] != ValI32 || decls[1].Results[0] != ValF32 {
		t.Fatalf("same-named import metadata = %#v", decls)
	}
	calls := 0
	host := HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
		if calls%2 == 0 {
			results[0] = I32(1)
		} else {
			results[0] = F32(2)
		}
		calls++
	})
	in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{".": host}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for attempt := 0; attempt < 2; attempt++ {
		got, err := in.Call(context.Background(), "run")
		if err != nil || len(got) != 1 || got[0].I32() != 3 {
			t.Fatalf("run attempt %d = %v, %v; want 3", attempt, got, err)
		}
	}
	if calls != 4 {
		t.Fatalf("host import calls = %d, want 4 distinct indexed dispatches", calls)
	}
}

// Port of tests/all/traps.rs::multithreaded_traps.
func TestWasmtimePortTrapsSurviveGoroutineMigration(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []corewasm.ValType{corewasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("trap", 0, 0),
			wasmtest.ExportEntry("value", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	in, err := Instantiate(MustCompile(mod))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	assertUnreachable := func(where string) error {
		_, err := in.Invoke("trap")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
			return fmt.Errorf("%s trap = %v, want TrapUnreachable", where, err)
		}
		return nil
	}
	if err := assertUnreachable("initial goroutine"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		if err := assertUnreachable("migrated goroutine"); err != nil {
			done <- err
			return
		}
		got, err := in.Invoke("value")
		if err != nil || len(got) != 1 || AsI32(got[0]) != 7 {
			done <- fmt.Errorf("post-trap value = %v, %v; want 7", got, err)
			return
		}
		done <- nil
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func wasmtimeReuseMemoryModule(failing bool) []byte {
	if !failing {
		return wasmtest.Module(
			wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
		)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(1), 0xaa)...))),
	)
}

func wasmtimeReuseTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]corewasm.ValType{corewasm.I32}, nil),
			wasmtest.FuncType([]corewasm.ValType{corewasm.I32}, []corewasm.ValType{corewasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x0a, 0x0a})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry("set", 0, 1),
			wasmtest.ExportEntry("is_null", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x26, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x25, 0x00, 0xd1, 0x0b}),
		)),
	)
}
