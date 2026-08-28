//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

// These tests adapt portable runtime and compiler regressions from Regression's
// tests/all at revision a5720e50d5ec9eab34eed690eee952abfdd0e3ba.
// Port of tests/all/pooling_allocator.rs::memory_zeroed.
func TestRuntimeRegressionPortReusedMemoryIsZeroed(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(regressionReuseMemoryModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	first, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	memory := first.Memory().UnsafeBytes()
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
	for i, b := range second.Memory().UnsafeBytes() {
		if b != 0 {
			t.Fatalf("reused memory byte %d = %#x, want zero", i, b)
		}
	}
}

// Port of tests/all/pooling_allocator.rs::table_zeroed.
func TestRuntimeRegressionPortReusedFuncrefTableIsZeroed(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	compiled := MustCompile(regressionReuseTableModule())
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
func TestRuntimeRegressionPortFailedInstantiationMemoryDoesNotLeak(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	clean, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(regressionReuseMemoryModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer clean.Close()
	failing, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(regressionReuseMemoryModule(true))
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
	for i, b := range after.Memory().UnsafeBytes() {
		if b != 0 {
			t.Fatalf("memory byte %d after failed instantiation = %#x, want zero", i, b)
		}
	}
}

// Port of misc_testsuite/pooling-oob-on-reuse.wast. In addition to checking
// the public trap behavior, require the one-slot cache to hand the exact grown
// mapping to the smaller module so the stale-bounds regression cannot pass
// vacuously after reuse is accidentally disabled.
func TestRuntimeRegressionPortMemoryReuseKeepsBounds(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	growCode := compileRegressionDirectFixture(t, "pooling-oob-on-reuse", 0)
	defer growCode.Close()
	oobCode := compileRegressionDirectFixture(t, "pooling-oob-on-reuse", 1)
	defer oobCode.Close()

	type growResult struct {
		memory *coreruntime.JobMemory
		err    error
	}
	for i := 0; i < 32; i++ {
		done := make(chan growResult, 1)
		go func() {
			in, err := Instantiate(growCode)
			if err == nil {
				_, err = in.Invoke("grow")
			}
			var memory *coreruntime.JobMemory
			if in != nil {
				memory = in.jm
				if closeErr := in.Close(); err == nil {
					err = closeErr
				}
			}
			done <- growResult{memory: memory, err: err}
		}()
		var grown growResult
		select {
		case grown = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d grow/drop module timed out", i)
		}
		if grown.err != nil {
			t.Fatalf("iteration %d grow/drop module: %v", i, grown.err)
		}

		in, err := Instantiate(oobCode)
		if err != nil {
			t.Fatalf("iteration %d instantiate bounds module: %v", i, err)
		}
		if in.jm != grown.memory {
			_ = in.Close()
			t.Fatalf("iteration %d JobMemory was not reused: grown=%p bounds=%p", i, grown.memory, in.jm)
		}
		_, err = in.Invoke("read_oob")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
			_ = in.Close()
			t.Fatalf("iteration %d read_oob = %v, want TrapLinMemOutOfBounds", i, err)
		}
		if err := in.Close(); err != nil {
			t.Fatalf("iteration %d close bounds module: %v", i, err)
		}
	}
}

// Port of tests/all/module.rs::large_add_chain_no_stack_overflow.
func TestRuntimeRegressionPortLargeAddChainDoesNotOverflowCompilerStack(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
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
func TestRuntimeRegressionPortParallelValidationErrorIsDeterministic(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
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

// Port of tests/all/import_indexes.rs::same_import_names_still_distinct. The
// upstream oracle is import metadata identity; Wago's public Imports map binds
// duplicate keys to one host value, so the execution below is only a call-site
// smoke test and does not claim independently bindable duplicate imports.
func TestRuntimeRegressionPortSameNamedImportDeclarationsRemainDistinct(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
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
	defer mod.Compiled().Close()
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
		t.Fatalf("host import calls = %d, want 4", calls)
	}
}

// Port of tests/all/traps.rs::multithreaded_traps. The upstream case performs
// one cross-thread handoff; this adaptation also runs independent instances in
// parallel, preserving Instance's documented non-concurrent-call contract while
// stressing process-wide trap state and post-trap recovery.
func TestRuntimeRegressionPortTrapsSurviveConcurrentGoroutines(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
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
	// Trap-state concurrency is independent of memory bounds. Force explicit
	// mode so a guard-page build does not reserve one multi-gigabyte guard range
	// per simultaneously live worker instance.
	compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(mod)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	const workers = 10
	instances := make([]*Instance, workers+1)
	for i := range instances {
		in, err := Instantiate(compiled)
		if err != nil {
			t.Fatalf("instantiate trap worker %d: %v", i, err)
		}
		instances[i] = in
		defer in.Close()
	}

	run := func(in *Instance, where string, iterations int) error {
		for i := 0; i < iterations; i++ {
			_, err := in.Invoke("trap")
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
				return fmt.Errorf("%s iteration %d trap = %v, want TrapUnreachable", where, i, err)
			}
		}
		got, err := in.Invoke("value")
		if err != nil || len(got) != 1 || AsI32(got[0]) != 7 {
			return fmt.Errorf("%s post-trap value = %v, %v; want 7", where, got, err)
		}
		return nil
	}

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			if err := run(instances[i], fmt.Sprintf("worker %d", i), 100); err != nil {
				errCh <- err
			}
		}()
	}
	close(start)
	if err := run(instances[workers], "caller", 1_000); err != nil {
		t.Error(err)
	}
	workersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent trap workers timed out")
	}
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestRuntimeRegressionPortResourceFootprintRemainsBounded(t *testing.T) {
	if runRegressionIsolatedPortTest(t) {
		return
	}
	compiled, err := NewRuntimeConfig().Compile(regressionReuseMemoryModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	run := func(iterations int) {
		for i := 0; i < iterations; i++ {
			in, err := Instantiate(compiled)
			if err != nil {
				t.Fatalf("iteration %d instantiate: %v", i, err)
			}
			if err := in.Close(); err != nil {
				t.Fatalf("iteration %d close: %v", i, err)
			}
		}
	}
	// Warm the runtime pools and the race detector's lazily mapped bookkeeping
	// before taking the process-map baseline. Small code-size changes can otherwise
	// make the detector add one shadow arena during the measured loop and look like
	// a leaked Wago mapping.
	run(64)
	runtime.GC()
	baseGoroutines := runtime.NumGoroutine()
	baseFDs, baseMaps := regressionProcessResourceCounts()
	run(256)
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	gotGoroutines := runtime.NumGoroutine()
	gotFDs, gotMaps := regressionProcessResourceCounts()
	if gotGoroutines > baseGoroutines+2 {
		t.Fatalf("goroutines grew from %d to %d after repeated instances", baseGoroutines, gotGoroutines)
	}
	if baseFDs >= 0 && gotFDs > baseFDs+1 {
		t.Fatalf("file descriptors grew from %d to %d after repeated instances", baseFDs, gotFDs)
	}
	if baseMaps >= 0 && gotMaps > baseMaps+4 {
		t.Fatalf("memory mappings grew from %d to %d after repeated instances", baseMaps, gotMaps)
	}
}

func regressionProcessResourceCounts() (fds, mappings int) {
	fds, mappings = -1, -1
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		fds = len(entries)
	}
	if data, err := os.ReadFile("/proc/self/maps"); err == nil {
		mappings = strings.Count(string(data), "\n")
	}
	return
}

func compileRegressionDirectFixture(t *testing.T, fixture string, module int) *Compiled {
	t.Helper()
	path := filepath.Join("../../tests/regressions/runtime/core", fixture, fmt.Sprintf("module.%d.wasm", module))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func regressionReuseMemoryModule(failing bool) []byte {
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

func regressionReuseTableModule() []byte {
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
