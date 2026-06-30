//go:build linux && amd64 && wago_guardpage

package amd64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runGuarded compiles function 0 with bounds-check elision, backs it with a
// guard-page memory + the SIGSEGV trap handler, and runs it. An out-of-bounds
// access faults into the handler and returns as a *TrapError instead of an
// inline check.
func runGuarded(t *testing.T, m *wasm.Module, setup func([]byte), arg int32) (int32, error) {
	t.Helper()
	if err := runtime.InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	ElideBoundsChecks = true
	code, err := CompileFunction(m, 0)
	ElideBoundsChecks = false
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemoryGuarded(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	lin := jm.LinearMemory()
	if setup != nil {
		setup(lin)
	}
	mem, entry, err := runtime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, uint32(arg))
	err = eng.CallGuarded(entry, serArgs, lin, trap, results, jm)
	return int32(binary.LittleEndian.Uint32(results)), err
}

const loadMod = `(module (memory 1) (func (export "f") (param i32) (result i32) local.get 0 i32.load))`

// TestGuardPageInBounds exercises elision + guard-page memory with no fault.
func TestGuardPageInBounds(t *testing.T) {
	got, err := runGuarded(t, watToModule(t, loadMod), func(lin []byte) {
		binary.LittleEndian.PutUint32(lin[16:], 0xCAFEF00D)
	}, 16)
	if err != nil {
		t.Fatalf("in-bounds load trapped: %v", err)
	}
	if uint32(got) != 0xCAFEF00D {
		t.Fatalf("load = %#x, want 0xCAFEF00D", uint32(got))
	}
}

// TestGuardPageOOBTraps is the spike's payoff: an unchecked out-of-bounds load
// faults on a PROT_NONE guard page and the handler converts it to a wasm trap.
func TestGuardPageOOBTraps(t *testing.T) {
	_, err := runGuarded(t, watToModule(t, loadMod), nil, 1<<20) // 1 MiB, past the 64 KiB page
	var te *runtime.TrapError
	if !errors.As(err, &te) || te.Code != runtime.TrapLinMemOutOfBounds {
		t.Fatalf("expected out-of-bounds trap, got %v", err)
	}
}

// TestGuardPageBoundary checks page-exact trapping at the wasm memory end.
func TestGuardPageBoundary(t *testing.T) {
	const page = 65536
	cases := []struct {
		addr int32
		trap bool
	}{
		{page - 4, false}, // last in-range i32
		{page - 3, true},  // crosses into the guard page
		{page, true},      // first guard byte
		{page + 4096, true},
	}
	for _, c := range cases {
		_, err := runGuarded(t, watToModule(t, loadMod), nil, c.addr)
		var te *runtime.TrapError
		got := errors.As(err, &te)
		if got != c.trap {
			t.Fatalf("addr=%d: trapped=%v, want %v (err=%v)", c.addr, got, c.trap, err)
		}
	}
}

const storeMod = `(module (memory 1) (func (export "f") (param i32) (result i32)
	local.get 0 i32.const 0x42 i32.store offset=0
	i32.const 0))`

// TestGuardPageStoreOOB checks an unchecked OOB store also faults into a trap,
// and an in-range store still writes.
func TestGuardPageStoreOOB(t *testing.T) {
	_, err := runGuarded(t, watToModule(t, storeMod), nil, 1<<20)
	var te *runtime.TrapError
	if !errors.As(err, &te) || te.Code != runtime.TrapLinMemOutOfBounds {
		t.Fatalf("OOB store: expected trap, got %v", err)
	}
	// in-range store writes the cell
	if _, err := runGuarded(t, watToModule(t, storeMod), nil, 32); err != nil {
		t.Fatalf("in-range store trapped: %v", err)
	}
}

// TestGuardPageReuseAfterTrap runs in-bounds, OOB (trap), in-bounds again on one
// engine — the handler must be reusable and leave no broken state.
func TestGuardPageReuseAfterTrap(t *testing.T) {
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
	binary.LittleEndian.PutUint32(jm.LinearMemory()[64:], 0x99)
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
	call := func(addr int32) error {
		binary.LittleEndian.PutUint32(serArgs, uint32(addr))
		binary.LittleEndian.PutUint32(trap, 0)
		return eng.CallGuarded(entry, serArgs, jm.LinearMemory(), trap, results, jm)
	}
	for i := 0; i < 5; i++ {
		if err := call(64); err != nil {
			t.Fatalf("iter %d in-bounds trapped: %v", i, err)
		}
		if got := binary.LittleEndian.Uint32(results); got != 0x99 {
			t.Fatalf("iter %d load = %#x, want 0x99", i, got)
		}
		if err := call(1 << 20); err == nil {
			t.Fatalf("iter %d OOB did not trap", i)
		}
	}
}

const sumMod = `(module (memory 1) (func (export "f") (param i32 i32) (result i32) (local i32) (local i32)
	(block (loop
		local.get 2 local.get 1 i32.ge_s br_if 1
		local.get 3
		local.get 0 local.get 2 i32.const 4 i32.mul i32.add
		i32.load
		i32.add local.set 3
		local.get 2 i32.const 1 i32.add local.set 2
		br 0))
	local.get 3))`

// BenchmarkGuardPageMemSum compares an array-sum load loop with explicit bounds
// checks vs guard-page elision (same wasm, n=4096 loads/call).
func BenchmarkGuardPageMemSum(b *testing.B) {
	const n = 4096
	run := func(b *testing.B, guarded bool) {
		ElideBoundsChecks = guarded
		code, err := CompileFunction(watToModuleB(b, sumMod), 0)
		ElideBoundsChecks = false
		if err != nil {
			b.Fatal(err)
		}
		eng, _ := runtime.NewEngine()
		defer eng.Close()
		var jm *runtime.JobMemory
		if guarded {
			_ = runtime.InstallGuardTrapHandler()
			jm, _ = runtime.NewJobMemoryGuarded(1 << 16)
		} else {
			jm, _ = runtime.NewJobMemory(1 << 16)
		}
		defer jm.Close()
		ar, _ := runtime.NewArena(4096)
		defer ar.Close()
		mem, entry, _ := runtime.MapCode(code)
		defer runtime.Unmap(mem)
		serArgs, results, trap := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
		binary.LittleEndian.PutUint32(serArgs[8:], n) // arg1 = n (arg0 = ptr 0)
		lin := jm.LinearMemory()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if guarded {
				_ = eng.CallGuarded(entry, serArgs, lin, trap, results, jm)
			} else {
				_ = eng.Call(entry, serArgs, lin, trap, results)
			}
		}
	}
	b.Run("checked", func(b *testing.B) { run(b, false) })
	b.Run("guarded", func(b *testing.B) { run(b, true) })
}
