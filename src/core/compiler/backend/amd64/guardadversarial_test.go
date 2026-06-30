//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"encoding/binary"
	"errors"
	"runtime/debug"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runGuardedModule compiles a whole module (with internal calls) guarded and
// runs function 0.
func runGuardedModule(t *testing.T, m *wasm.Module, arg int32) (int32, error) {
	t.Helper()
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	ElideBoundsChecks = true
	cm, err := CompileModule(m)
	ElideBoundsChecks = false
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemoryGuarded(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, base, _ := runtime.MapCode(cm.Code)
	defer runtime.Unmap(mem)
	serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, uint32(arg))
	err = eng.CallGuarded(base+uintptr(cm.Entry[0]), serArgs, jm.LinearMemory(), trap, results, jm)
	return int32(binary.LittleEndian.Uint32(results)), err
}

// TestGuardNestedFaultPropagates faults two calls deep; the trap must propagate
// out through the post-call trap checks, not crash.
func TestGuardNestedFaultPropagates(t *testing.T) {
	m := watToModule(t, `(module (memory 1)
		(func (export "f") (param i32) (result i32) local.get 0 call 1)
		(func (param i32) (result i32) local.get 0 call 2)
		(func (param i32) (result i32) local.get 0 i32.load))`)
	_, err := runGuardedModule(t, m, 1<<20) // OOB load three frames deep
	var te *runtime.TrapError
	if !errors.As(err, &te) || te.Code != runtime.TrapLinMemOutOfBounds {
		t.Fatalf("nested OOB: expected trap, got %v", err)
	}
}

// TestGuardNestedInBounds confirms the call chain still returns values normally
// when nothing faults.
func TestGuardNestedInBounds(t *testing.T) {
	m := watToModule(t, `(module (memory 1)
		(func (export "f") (param i32) (result i32) local.get 0 call 1)
		(func (param i32) (result i32) local.get 0 i32.load))`)
	// memory is zeroed; an in-bounds load returns 0 with no trap.
	got, err := runGuardedModule(t, m, 16)
	if err != nil {
		t.Fatalf("nested in-bounds trapped: %v", err)
	}
	if got != 0 {
		t.Fatalf("nested load = %d, want 0", got)
	}
}

// TestGuardHandlerChainsGoFault is the critical safety check: with our SIGSEGV
// handler installed, a genuine Go nil dereference must STILL fault correctly
// (chain to Go's handler -> sigpanic), not be swallowed or crash the process.
func TestGuardHandlerChainsGoFault(t *testing.T) {
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	// Run a guarded call first so the reservation globals are set to a real range
	// (the nil address 0 must fall OUTSIDE it and chain).
	if _, err := runGuarded(t, watToModule(t, loadMod), nil, 1<<20); !errors.As(err, new(*runtime.TrapError)) {
		t.Fatalf("setup OOB call did not trap: %v", err)
	}
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("nil dereference did not panic with the guard handler installed")
		}
	}()
	var p *int
	_ = *p // SIGSEGV in Go code -> our handler -> not in reservation -> Go's handler -> panic
	t.Fatal("unreachable")
}

// TestGuardExtremeAddresses hammers the reservation edges: the maximum wasm32
// address and a large memarg offset must trap (stay inside the 8 GiB
// reservation), never escape to a foreign mapping or crash.
func TestGuardExtremeAddresses(t *testing.T) {
	cases := []struct {
		name string
		mod  string
		arg  int32
	}{
		{"max u32 addr", loadMod, -1}, // addr = 0xFFFFFFFF
		{"addr + 2GiB offset", `(module (memory 1) (func (export "f") (param i32) (result i32)
			local.get 0 i32.load offset=2147483647))`, -1},
		{"high addr near 4GiB", loadMod, int32(-65536)}, // 0xFFFF0000
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runGuarded(t, watToModule(t, c.mod), nil, c.arg)
			if !errors.As(err, new(*runtime.TrapError)) {
				t.Fatalf("%s: expected trap, got %v", c.name, err)
			}
		})
	}
}

// TestGuardConcurrent runs guarded OOB calls from many goroutines. CallGuarded
// serialises on a mutex, but the handler + globals must survive concurrent entry
// with no crash, race, or lost/garbled trap.
func TestGuardConcurrent(t *testing.T) {
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	ElideBoundsChecks = true
	code, err := CompileFunction(watToModule(t, loadMod), 0)
	ElideBoundsChecks = false
	if err != nil {
		t.Fatal(err)
	}
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	const G = 8
	var wg sync.WaitGroup
	errs := make([]error, G)
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			eng, _ := runtime.NewEngine()
			defer eng.Close()
			jm, _ := runtime.NewJobMemoryGuarded(1 << 16)
			defer jm.Close()
			ar, _ := runtime.NewArena(4096)
			defer ar.Close()
			serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
			lin := jm.LinearMemory()
			for i := 0; i < 100; i++ {
				binary.LittleEndian.PutUint32(serArgs, 1<<20) // OOB
				binary.LittleEndian.PutUint32(trap, 0)
				if e := eng.CallGuarded(entry, serArgs, lin, trap, results, jm); !errors.As(e, new(*runtime.TrapError)) {
					errs[idx] = e
					return
				}
				// interleave an in-bounds call
				binary.LittleEndian.PutUint32(serArgs, 8)
				binary.LittleEndian.PutUint32(trap, 0)
				if e := eng.CallGuarded(entry, serArgs, lin, trap, results, jm); e != nil {
					errs[idx] = e
					return
				}
			}
		}(g)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d: %v", i, e)
		}
	}
}

// TestGuardStraddleStorePartialWrite probes a store that straddles the guard
// boundary: bounds-checked mode traps with NO write; guard-page mode may let the
// CPU commit the in-range bytes before faulting on the guard page — an
// observable wasm-semantics divergence. This documents the actual behaviour.
func TestGuardStraddleStorePartialWrite(t *testing.T) {
	const page = 65536
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	// i64.store (8 bytes) at addr=page-4 writes [page-4, page+4): half committed,
	// half guard.
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32)
		local.get 0 i64.const -1 i64.store))`)
	ElideBoundsChecks = true
	code, err := CompileFunction(m, 0)
	ElideBoundsChecks = false
	if err != nil {
		t.Fatal(err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemoryGuarded(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	lin := jm.LinearMemory()
	for i := page - 8; i < page; i++ {
		lin[i] = 0xEE // sentinel
	}
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, page-4)
	err = eng.CallGuarded(entry, serArgs, lin, trap, results, jm)
	if !errors.As(err, new(*runtime.TrapError)) {
		t.Fatalf("straddling store: expected trap, got %v", err)
	}
	// Did the in-range half [page-4, page) get clobbered before the fault?
	partial := false
	for i := page - 4; i < page; i++ {
		if lin[i] != 0xEE {
			partial = true
		}
	}
	t.Logf("straddling i64.store at page-4: in-range bytes partially written = %v "+
		"(bounds-checked mode writes nothing)", partial)
	// The lower sentinel [page-8, page-4) must be intact regardless.
	for i := page - 8; i < page-4; i++ {
		if lin[i] != 0xEE {
			t.Fatalf("byte %d outside the access was modified: %#x", i, lin[i])
		}
	}
}

// TestGuardDeepRecursionFault faults at the bottom of a deep recursion; the trap
// must propagate up through ~2000 post-call checks without corrupting the unwind.
func TestGuardDeepRecursionFault(t *testing.T) {
	m := watToModule(t, `(module (memory 1)
		(func (export "f") (param i32) (result i32)
			local.get 0 i32.eqz
			if (result i32)
				i32.const 1000000 i32.load
			else
				local.get 0 i32.const 1 i32.sub call 0
			end))`)
	_, err := runGuardedModule(t, m, 2000)
	if !errors.As(err, new(*runtime.TrapError)) {
		t.Fatalf("deep-recursion OOB: expected trap, got %v", err)
	}
}

// TestGuardGCStress hammers OOB+in-bounds calls while allocating garbage to force
// GC and async preemption concurrent with the fault-handling path.
func TestGuardGCStress(t *testing.T) {
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	ElideBoundsChecks = true
	code, err := CompileFunction(watToModule(t, loadMod), 0)
	ElideBoundsChecks = false
	if err != nil {
		t.Fatal(err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemoryGuarded(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	lin := jm.LinearMemory()
	binary.LittleEndian.PutUint32(lin[8:], 0x1234)
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	sink := make([][]byte, 0, 64)
	for i := 0; i < 20000; i++ {
		binary.LittleEndian.PutUint32(serArgs, 1<<20)
		binary.LittleEndian.PutUint32(trap, 0)
		if e := eng.CallGuarded(entry, serArgs, lin, trap, results, jm); !errors.As(e, new(*runtime.TrapError)) {
			t.Fatalf("iter %d OOB did not trap: %v", i, e)
		}
		binary.LittleEndian.PutUint32(serArgs, 8)
		binary.LittleEndian.PutUint32(trap, 0)
		if e := eng.CallGuarded(entry, serArgs, lin, trap, results, jm); e != nil {
			t.Fatalf("iter %d in-bounds trapped: %v", i, e)
		}
		if got := binary.LittleEndian.Uint32(results); got != 0x1234 {
			t.Fatalf("iter %d load = %#x, want 0x1234", i, got)
		}
		sink = append(sink, make([]byte, 256)) // GC pressure
		if len(sink) == 64 {
			sink = sink[:0]
		}
	}
}
