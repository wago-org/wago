//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runMem compiles function 0, sets up linear memory, executes, and returns the
// result, the post-execution linear memory, and any trap error.
func runMem(t *testing.T, m *wasm.Module, setup func([]byte), args ...int32) (int32, []byte, error) {
	t.Helper()
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(1 << 16)
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
	for i, a := range args {
		binary.LittleEndian.PutUint32(serArgs[i*8:], uint32(a))
	}
	err = eng.Call(entry, serArgs, lin, trap, results)
	// Copy linear memory before the deferred Close() unmaps it.
	return int32(binary.LittleEndian.Uint32(results)), append([]byte(nil), lin...), err
}

func TestMemoryArraySum(t *testing.T) {
	// sum the n i32s starting at ptr.
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32) (result i32) (local i32) (local i32)
		(block (loop
			local.get 2 local.get 1 i32.ge_s br_if 1
			local.get 3
			local.get 0 local.get 2 i32.const 4 i32.mul i32.add
			i32.load
			i32.add local.set 3
			local.get 2 i32.const 1 i32.add local.set 2
			br 0))
		local.get 3))`)
	vals := []int32{10, 20, 30, 40, 50}
	got, _, err := runMem(t, m, func(lin []byte) {
		for i, v := range vals {
			binary.LittleEndian.PutUint32(lin[i*4:], uint32(v))
		}
	}, 0, int32(len(vals)))
	if err != nil {
		t.Fatal(err)
	}
	if got != 150 {
		t.Fatalf("array sum = %d, want 150", got)
	}
}

func TestMemoryStoreLoad(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.store
		local.get 0 i32.load))`)
	got, lin, err := runMem(t, m, nil, 256, 0x1234ABCD)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x1234ABCD {
		t.Fatalf("store/load = %#x, want 0x1234ABCD", got)
	}
	if v := binary.LittleEndian.Uint32(lin[256:]); v != 0x1234ABCD {
		t.Fatalf("linMem[256] = %#x, want 0x1234ABCD", v)
	}
}

func TestMemoryByteAccess(t *testing.T) {
	// store8 then load8_u, and load8_s for sign.
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.store8
		local.get 0 i32.load8_u))`)
	got, lin, err := runMem(t, m, nil, 10, 0xFF)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0xFF {
		t.Fatalf("load8_u = %d, want 255", got)
	}
	if lin[10] != 0xFF {
		t.Fatalf("linMem[10] = %#x, want 0xFF", lin[10])
	}

	ms := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.store8
		local.get 0 i32.load8_s))`)
	got, _, err = runMem(t, ms, nil, 10, 0xFF)
	if err != nil {
		t.Fatal(err)
	}
	if got != -1 {
		t.Fatalf("load8_s of 0xFF = %d, want -1", got)
	}
}

func TestMemoryBoundsTrap(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32) (result i32)
		local.get 0 i32.load))`)
	// 64 KiB memory; access at 1_000_000 must trap.
	_, _, err := runMem(t, m, nil, 1000000)
	var te *runtime.TrapError
	if !errors.As(err, &te) {
		t.Fatalf("expected out-of-bounds TrapError, got %v", err)
	}
	if te.Code != runtime.TrapLinMemOutOfBounds {
		t.Fatalf("trap code = %v, want %v", te.Code, runtime.TrapLinMemOutOfBounds)
	}
}
