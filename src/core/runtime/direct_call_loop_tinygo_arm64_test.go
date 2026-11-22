//go:build (darwin || linux) && arm64 && tinygo && !scheduler.threads

package runtime_test

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestTaskNativeDirectCallLoopKeepsEntryInterrupt(t *testing.T) {
	caller := []byte{0, 0x03, 0x40, 0x20, 0, 0x10, 1, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}
	callee := []byte{0, 0x20, 0, 0x41, 1, 0x6b, 0x0b}
	sig := []wasm.ValType{wasm.I32}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
		)),
	)
	checkTaskNativeEntry(t, b, 0, false)
}

func TestTaskNativeRecursiveEntryKeepsStackFenceAndInterrupt(t *testing.T) {
	// count(n) returns 1 + count(n - 1) for n > 0, and 0 otherwise.
	body := []byte{0, 0x20, 0, 0x04, 0x7f, 0x20, 0, 0x41, 1, 0x6b, 0x10, 0, 0x41, 1, 0x6a, 0x05, 0x41, 0, 0x0b, 0x0b}
	sig := []wasm.ValType{wasm.I32}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	checkTaskNativeEntry(t, b, 8, true)
}

func checkTaskNativeEntry(t *testing.T, b []byte, want uint64, recursive bool) {
	t.Helper()
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := arm64.CompileModuleWith(m, arm64.CompileOptions{Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	engine, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	memory, err := runtime.NewJobMemory(1 << 16)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	memory.SetStackFence(engine.StackLimit())
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	args, results := arena.Alloc(8), arena.Alloc(8)
	trap := arena.Alloc(runtime.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, 8)
	root := entry + uintptr(cm.Entry[0])
	if err := engine.Call(root, args, memory.LinearMemory(), trap, results); err != nil || binary.LittleEndian.Uint64(results) != want {
		t.Fatalf("native result %d, want %d, error %v", binary.LittleEndian.Uint64(results), want, err)
	}
	if recursive {
		binary.LittleEndian.PutUint64(args, 1<<26)
		err := engine.Call(root, args, memory.LinearMemory(), trap, results)
		if trapped, ok := err.(*runtime.TrapError); !ok || trapped.Code != runtime.TrapStackFenceBreached {
			t.Fatalf("deep recursion returned %v, want stack fence trap", err)
		}
		binary.LittleEndian.PutUint64(args, 8)
	}
	binary.LittleEndian.PutUint64(results, 99)
	runtime.RequestInterrupt(trap)
	err = engine.Call(root, args, memory.LinearMemory(), trap, results)
	if trapped, ok := err.(*runtime.TrapError); !ok || trapped.Code != runtime.TrapInterrupted || binary.LittleEndian.Uint64(results) != 99 {
		t.Fatalf("pending interrupt changed result to %d or produced wrong error %v", binary.LittleEndian.Uint64(results), err)
	}
}
